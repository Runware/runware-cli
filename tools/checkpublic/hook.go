package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// hookInput is the part of a PreToolUse payload the hook reads. Claude Code
// and Codex send the same fields: tool_name, tool_input (for Bash, its
// command; for an MCP tool, its arguments) and cwd.
type hookInput struct {
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
	Cwd       string          `json:"cwd"`
}

// hookToolInput is the tool_input of a Bash call.
type hookToolInput struct {
	Command string `json:"command"`
}

// publication is text a command would post to GitHub.
type publication struct {
	label string
	text  string
}

// pushSpec is a git push found in a command line.
type pushSpec struct {
	dir  string
	args []string
}

// plan is what a command line would publish.
type plan struct {
	// texts are the files a gh write posts.
	texts  []publication
	pushes []pushSpec
	// scanLine is set when the line holds a gh write to a repository that
	// is not private: the whole command line is scanned, here-document
	// bodies included.
	scanLine bool
}

// repoResolver returns the owner/name of the GitHub repository a directory
// belongs to, or "" when it cannot tell.
type repoResolver func(dir string) string

// planCommand finds the git pushes and the gh writes in a Bash command line
// run from dir. It returns an UncheckableError when the line publishes, or
// may publish, in a form it cannot read completely.
func (s *Scanner) planCommand(command, dir string, repoOf repoResolver) (plan, error) {
	line := parseShell(command)
	cmds := commands(line)
	kinds := make([]pubKind, len(cmds))
	found := false
	for i := range cmds {
		c := &cmds[i]
		if where := hiddenPublication(*c, cmds, 0); where != "" {
			return plan{}, uncheckable("it runs a GitHub write or git push %s", where)
		}
		kinds[i] = publishes(c.words, c.wordDyn)
		switch kinds[i] {
		case pubUnknown:
			return plan{}, uncheckable("`%s` may be a GitHub write or git push the check cannot read", c)
		case pubGH, pubPush:
			found = true
		}
	}
	if !found {
		return plan{}, nil
	}
	if line.grouped {
		return plan{}, uncheckable("it runs a GitHub write or git push in a line with a ( ) group, a function or a case")
	}
	for i := range cmds {
		if _, ok := shellKeywords[cmds[i].name()]; ok {
			return plan{}, uncheckable("it runs a GitHub write or git push in a line with a loop, condition or { } block")
		}
	}

	var p plan
	cd := false
	cdUnknown := ""
	for i := range cmds {
		c := &cmds[i]
		switch c.name() {
		case "cd":
			if len(c.words) == 2 && !c.wordDyn[1] && c.words[1] != "-" {
				dir = resolvePath(dir, c.words[1])
				cd = true
			} else {
				cdUnknown = c.String()
			}
		case "pushd", "popd":
			cdUnknown = c.String()
		}
		if kinds[i] == pubNone {
			continue
		}
		if cdUnknown != "" {
			return plan{}, uncheckable("`%s` runs before it, and the check cannot tell which directory that leaves", cdUnknown)
		}
		pc := newPubContext(cmds, i, dir, cd)
		if kinds[i] == pubPush {
			push, err := pc.push()
			if err != nil {
				return plan{}, err
			}
			p.pushes = append(p.pushes, push)
			continue
		}
		texts, private, err := s.ghTexts(pc, repoOf)
		if err != nil {
			return plan{}, err
		}
		p.scanLine = p.scanLine || !private
		p.texts = append(p.texts, texts...)
	}
	return p, nil
}

// pubContext is a publication with what runs before it or alongside it in
// the same command line.
type pubContext struct {
	cmd  command
	line []command
	dir  string
	// cd is set when a cd ran before the publication.
	cd bool
	// writes are the output redirections of the commands that run before
	// the publication or alongside it, the publication's own included.
	writes []redirTarget
	// changer is the first command that runs before the publication or
	// alongside it and may write a file or move a ref, or "".
	changer string
}

func newPubContext(line []command, i int, dir string, cd bool) pubContext {
	pc := pubContext{
		cmd:    line[i],
		line:   line,
		dir:    dir,
		cd:     cd,
		writes: line[i].writes,
	}
	for _, j := range alongside(line, i) {
		pc.writes = append(pc.writes, line[j].writes...)
		if pc.changer == "" && !inert(line[j]) {
			pc.changer = line[j].String()
		}
	}
	return pc
}

// readFile reads a file a publication posts, or stdin for "-". It refuses
// a file the same command line may write first.
func (pc pubContext) readFile(name string) (string, error) {
	if name == "-" {
		return pc.readStdin()
	}
	if pc.changer != "" {
		return "", uncheckable("it posts %s, but `%s` runs before it or alongside it and may change that file", name, pc.changer)
	}
	if pc.cd && !filepath.IsAbs(name) && !strings.HasPrefix(name, "~") {
		return "", uncheckable("it posts %s by a path relative to a directory that cd changed", name)
	}
	for _, w := range pc.writes {
		if w.dynamic || filepath.Base(w.path) == filepath.Base(name) {
			return "", uncheckable("it posts %s, which a redirection in the same command may write first", name)
		}
	}
	path := resolvePath(pc.dir, name)
	st, err := os.Stat(path)
	if err != nil || !st.Mode().IsRegular() {
		return "", uncheckable("it posts %s, which does not exist yet or is not a regular file", name)
	}
	b, err := os.ReadFile(path) //nolint:gosec // the file the command posts
	if err != nil {
		return "", uncheckable("it posts %s, which the check cannot read: %v", name, err)
	}
	return string(b), nil
}

// readStdin returns what a publication reads on stdin. A here-document or
// here-string, its own or piped from cat, is text of the command line, which
// is scanned whole, so it returns "" for one.
func (pc pubContext) readStdin() (string, error) {
	c := pc.cmd
	sources := c.stdinSources
	if c.pipeFrom >= 0 {
		sources++
	}
	switch {
	case sources != 1:
		return "", uncheckable("it posts stdin, which %d pipes or redirections feed; give it exactly one", sources)
	case c.hasStdin && c.stdinDynamic:
		return "", uncheckable("it posts a here-document that expands variables or commands; quote its delimiter (<<'EOF')")
	case c.hasStdin:
		return "", nil
	case c.stdinFile != "" && !c.stdinFileDynamic:
		return pc.readFile(c.stdinFile)
	case c.pipeFrom >= 0 && pc.line[c.pipeFrom].inlineHeredoc():
		return "", nil
	}
	return "", uncheckable("it posts stdin from a source the check cannot read; use a file or a quoted here-document")
}

// push returns the git push of a publication, refusing forms whose target
// the check cannot work out.
func (pc pubContext) push() (pushSpec, error) {
	c := pc.cmd
	if anyTrue(c.wordDyn) || anyTrue(c.assignDyn) {
		return pushSpec{}, uncheckable("`%s` has a variable, command substitution or glob in its arguments", c)
	}
	if setsSensitive(c.assigns) {
		return pushSpec{}, uncheckable("`%s` sets a variable that changes which repository git uses", c)
	}
	dir := pc.dir
	i := 1
	for ; c.words[i] != wordPush; i++ {
		switch c.words[i] {
		case "-C":
			i++
			dir = resolvePath(dir, c.words[i])
		case "--no-pager":
		default:
			return pushSpec{}, uncheckable("`%s` has git options before push that the check does not follow", c)
		}
	}
	if pc.changer != "" {
		return pushSpec{}, uncheckable("`%s` runs before the push or alongside it and may change what it pushes; run the push in its own call", pc.changer)
	}
	return pushSpec{
		dir:  dir,
		args: c.words[i+1:],
	}, nil
}

// ghTexts returns the files a gh write posts, and whether its destination is
// a private Runware repository, where it publishes nothing to check. The rest
// of what it posts is text of the command line, which is scanned whole.
func (s *Scanner) ghTexts(pc pubContext, repoOf repoResolver) ([]publication, bool, error) {
	c := pc.cmd
	if anyTrue(c.assignDyn) {
		return nil, false, uncheckable("`%s` sets a variable from an expansion", c)
	}
	for i := range c.words {
		if c.wordDyn[i] && !inLine(c.wordSubs[i]) {
			return nil, false, uncheckable("`%s` uses a variable, glob or command substitution; only $(cat <<'EOF' ... EOF) is read", c)
		}
	}
	args := c.words[1:]
	group, verb, _ := ghCommand(c.words, c.wordDyn)
	valueFlags := apiValueFlags
	if group != wordAPI {
		valueFlags = "repo/R " + ghValueFlags[group+" "+verb]
	}
	flags, pos := parseFlags(args, valueFlags)
	for _, i := range pos {
		if c.wordDyn[i+1] {
			return nil, false, uncheckable("`%s` takes a positional argument from a substitution", c)
		}
	}
	label := "gh " + group + " " + verb
	if group == wordAPI {
		label = "gh api"
	}
	repos := pc.defaultRepos(repoOf)
	var texts []publication
	for _, f := range flags {
		file := ""
		switch {
		case f.name == "repo":
			repos = []string{f.value}
		case strings.HasSuffix(f.name, "-file") || f.name == "input":
			file = f.value
		case f.name == "field":
			_, v, _ := strings.Cut(f.value, "=")
			file, _ = strings.CutPrefix(v, "@")
			if file == v {
				file = ""
			}
		}
		if file == "" {
			continue
		}
		text, err := pc.readFile(file)
		if err != nil {
			return nil, false, err
		}
		texts = append(texts, publication{
			label: label + " " + f.name,
			text:  text,
		})
	}
	switch {
	case group == wordAPI:
		// A call outside repos/OWNER/NAME, such as graphql, may go anywhere.
		endpoint := ""
		if len(pos) > 1 {
			endpoint = args[pos[1]]
		}
		if m := apiRepoPath.FindStringSubmatch(endpoint); m == nil {
			repos = []string{""}
		} else if !strings.Contains(m[1], "{") {
			repos = []string{m[1]}
		}
	case verb != "create" && group != wordRelease && len(pos) > 2:
		// A pull request or issue URL names the destination, whatever -R
		// says.
		var dests []string
		for _, i := range pos[2:] {
			if r := ghURLRepo(args[i]); r != "" {
				dests = append(dests, r)
			} else {
				dests = append(dests, repos...)
			}
		}
		repos = dests
	}
	if s.allPrivate(repos) {
		return nil, true, nil
	}
	return texts, false, nil
}

var apiRepoPath = regexp.MustCompile(`^/?repos/([^/]+/[^/]+)`)

// defaultRepos returns where a gh command without -R or a URL posts: the
// GH_REPO it is given, or else the repository of its directory and any
// GH_REPO of the environment. "" stands for one the check cannot tell.
func (pc pubContext) defaultRepos(repoOf repoResolver) []string {
	for _, a := range pc.cmd.assigns {
		if v, ok := strings.CutPrefix(a, "GH_REPO="); ok {
			return []string{v}
		}
	}
	repos := []string{repoOf(pc.dir)}
	if v := os.Getenv("GH_REPO"); v != "" {
		repos = append(repos, v)
	}
	for i := range pc.line {
		if strings.Contains(strings.Join(pc.line[i].args, " "), "GH_REPO") {
			repos = append(repos, "")
		}
	}
	return repos
}

// allPrivate reports whether every one of repos is a private Runware
// repository. An unknown one ("") counts as public.
func (s *Scanner) allPrivate(repos []string) bool {
	for _, r := range repos {
		if !s.isPrivateTarget(r) {
			return false
		}
	}
	return len(repos) > 0
}

// isPrivateTarget reports whether owner/name is a Runware repository that is
// not public. Posting there is not this check's concern.
func (s *Scanner) isPrivateTarget(repo string) bool {
	repo = strings.TrimSuffix(repo, ".git")
	if i := strings.LastIndex(repo, "github.com/"); i >= 0 {
		repo = repo[i+len("github.com/"):]
	}
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || !strings.EqualFold(owner, "runware") {
		return false
	}
	return !s.IsPublicRepo(name)
}

// runHook reads a PreToolUse payload and returns the findings in what the
// Bash command or GitHub tool call would publish.
func (s *Scanner) runHook(ctx context.Context, payload []byte, repoOf repoResolver) ([]Finding, error) {
	var in hookInput
	if err := json.Unmarshal(payload, &in); err != nil {
		return nil, fmt.Errorf("read hook payload: %w", err)
	}
	if isGitHubWriteTool(in.ToolName) {
		return s.scanToolCall(in.ToolName, in.ToolInput)
	}
	if in.ToolName != "" && in.ToolName != "Bash" {
		return nil, nil
	}
	var bash hookToolInput
	if len(in.ToolInput) > 0 {
		if err := json.Unmarshal(in.ToolInput, &bash); err != nil {
			return nil, fmt.Errorf("read hook payload: %w", err)
		}
	}
	dir := in.Cwd
	if dir == "" {
		dir, _ = os.Getwd()
	}
	p, err := s.planCommand(bash.Command, dir, repoOf)
	if err != nil {
		// A line the check cannot read is blocked either way; lead with
		// what its text shows when that is a finding.
		if f := s.ScanText("command line", bash.Command); len(f) > 0 {
			return f, nil
		}
		return nil, err
	}
	var out []Finding
	if p.scanLine {
		out = append(out, s.ScanText("command line", bash.Command)...)
	}
	for _, t := range p.texts {
		out = append(out, s.ScanText(t.label, t.text)...)
	}
	for _, push := range p.pushes {
		f, err := s.scanPush(ctx, push)
		out = append(out, f...)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

// pushArgs splits git push arguments into the positional ones and the options.
func pushArgs(args []string) (pos, opts []string) {
	for i := 0; i < len(args); i++ {
		switch a := args[i]; {
		case a == "-o" || a == "--push-option" || a == "--repo" || a == "--receive-pack" || a == "--exec":
			i++
		case strings.HasPrefix(a, "-"):
			opts = append(opts, a)
		default:
			pos = append(pos, a)
		}
	}
	return pos, opts
}

// pushRemote returns the remote a git push goes to: the one it names, or the
// current branch's remote, or origin.
func pushRemote(ctx context.Context, g Git, pos []string) string {
	if len(pos) > 0 {
		return pos[0]
	}
	if current := g.CurrentBranch(ctx); current != "" {
		if out, err := g.run(ctx, "config", "branch."+current+".remote"); err == nil && strings.TrimSpace(out) != "" {
			return strings.TrimSpace(out)
		}
	}
	return "origin"
}

// scanPush scans what a git push would publish: for each pushed branch, what
// it has beyond its remote-tracking branch, or beyond its merge-base with main
// when the remote does not have it yet; and each pushed tag, named directly,
// as refs/tags/NAME, as `tag NAME`, or with --tags or --follow-tags.
func (s *Scanner) scanPush(ctx context.Context, p pushSpec) ([]Finding, error) {
	g := Git{
		Dir: p.dir,
	}
	pos, opts := pushArgs(p.args)
	remote := pushRemote(ctx, g, pos)
	if s.remoteIsPrivate(ctx, g, remote) {
		return nil, nil
	}
	current := g.CurrentBranch(ctx)
	var refspecs []string
	for i := 1; i < len(pos); i++ {
		if pos[i] == wordTag && i+1 < len(pos) {
			i++
			pos[i] = "refs/tags/" + pos[i]
		}
		refspecs = append(refspecs, pos[i])
	}
	has := func(o ...string) bool {
		return slices.ContainsFunc(opts, func(x string) bool { return slices.Contains(o, x) })
	}
	allTags := has("--tags")
	followTags := has("--follow-tags")
	if !followTags && !has("--no-follow-tags") {
		if out, err := g.run(ctx, "config", "--bool", "push.followTags"); err == nil && strings.TrimSpace(out) == "true" {
			followTags = true
		}
	}
	if has("--all", "--branches", "--mirror") {
		out, err := g.run(ctx, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
		if err != nil {
			return nil, err
		}
		refspecs = strings.Fields(out)
	}
	// git push --tags alone pushes only the tags.
	if len(refspecs) == 0 && current != "" && !allTags {
		refspecs = []string{current}
	}
	if allTags {
		out, err := g.run(ctx, "for-each-ref", "--format=%(refname)", "refs/tags/")
		if err != nil {
			return nil, err
		}
		refspecs = append(refspecs, strings.Fields(out)...)
	}

	var out []Finding
	var heads []string
	tags := map[string]string{}
	var tagOrder []string
	addTag := func(name, object string) {
		if _, ok := tags[name]; !ok {
			tags[name] = object
			tagOrder = append(tagOrder, name)
		}
	}
	for _, spec := range refspecs {
		src, dst, hasDst := strings.Cut(strings.TrimPrefix(spec, "+"), ":")
		if src == "" {
			continue // deleting a remote ref publishes nothing
		}
		if tag, ok := g.tagName(ctx, src, dst, hasDst); ok {
			if object, err := g.run(ctx, "rev-parse", "--verify", "--quiet", src); err == nil {
				addTag(tag, strings.TrimSpace(object))
			}
			continue
		}
		if src == "HEAD" && current != "" {
			src = current
		}
		if !hasDst || dst == "" {
			dst = src
		}
		dst = strings.TrimPrefix(dst, "refs/heads/")
		head, err := g.revParse(ctx, src)
		if err != nil {
			continue // not a commit: git push will say
		}
		tracking, _ := g.revParse(ctx, "refs/remotes/"+remote+"/"+dst)
		base, err := g.PushBase(ctx, remote, tracking, head)
		if err != nil {
			return out, err
		}
		f, err := s.ScanRange(ctx, g, Range{
			Base:   base,
			Head:   head,
			Branch: dst,
			Remote: remote,
		})
		if err != nil {
			return out, err
		}
		out = append(out, f...)
		heads = append(heads, head)
	}
	// --follow-tags adds the annotated tags on commits a pushed branch
	// reaches. Lightweight tags, and tags on commits not pushed, stay behind.
	for _, head := range heads {
		if !followTags || allTags {
			break
		}
		list, err := g.run(ctx, "for-each-ref", "--merged="+head, "--format=%(objecttype) %(objectname) %(refname)", "refs/tags/")
		if err != nil {
			return out, err
		}
		for l := range strings.SplitSeq(strings.TrimSpace(list), "\n") {
			if fields := strings.Fields(l); len(fields) == 3 && fields[0] == wordTag {
				addTag(strings.TrimPrefix(fields[2], "refs/tags/"), fields[1])
			}
		}
	}
	for _, name := range tagOrder {
		f, err := s.ScanTag(ctx, g, remote, name, tags[name])
		if err != nil {
			return out, err
		}
		out = append(out, f...)
	}
	return out, nil
}

// tagName returns the tag a refspec src[:dst] pushes, if it pushes one: the
// destination is under refs/tags/, or the source is a tag (git resolves a
// short name to a tag before a branch).
func (g Git) tagName(ctx context.Context, src, dst string, hasDst bool) (string, bool) {
	if name, ok := strings.CutPrefix(dst, "refs/tags/"); ok {
		return name, true
	}
	if hasDst && dst != "" {
		return "", false
	}
	if name, ok := strings.CutPrefix(src, "refs/tags/"); ok {
		return name, true
	}
	if !strings.HasPrefix(src, "refs/") && src != "HEAD" && g.hasRef(ctx, "refs/tags/"+src) {
		return src, true
	}
	return "", false
}

var remoteURL = regexp.MustCompile(`github\.com[:/]([^/]+/[^/]+?)(?:\.git)?/?$`)

// githubRepo returns the owner/name of a GitHub remote URL, or "".
func githubRepo(url string) string {
	m := remoteURL.FindStringSubmatch(strings.TrimSpace(url))
	if m == nil {
		return ""
	}
	return m[1]
}

// gitRepoOf reads the GitHub repository of dir from its origin remote.
func gitRepoOf(dir string) string {
	out, err := Git{Dir: dir}.run(context.Background(), "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return githubRepo(out)
}

// remoteIsPrivate reports whether a remote (a name or a URL) is a private
// Runware repository, where publishing is not this check's concern.
func (s *Scanner) remoteIsPrivate(ctx context.Context, g Git, remote string) bool {
	url := remote
	if out, err := g.run(ctx, "remote", "get-url", remote); err == nil {
		url = out
	}
	return s.isPrivateTarget(githubRepo(url))
}
