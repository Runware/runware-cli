package main

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
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
	texts  []publication
	pushes []pushSpec
	// scanLine is set when the line holds a gh write to a repository that
	// is not private: the whole command line is scanned too, here-document
	// bodies included.
	scanLine bool
}

// repoResolver returns the owner/name of the GitHub repository a directory
// belongs to, or "" when it cannot tell.
type repoResolver func(dir string) string

// planCommand finds the git pushes and the GitHub titles, bodies and comments
// in a Bash command line run from dir. It returns an UncheckableError when
// the line publishes, or may publish, in a form it cannot read completely.
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
	vars := shellVars{}
	for i := range cmds {
		c := &cmds[i]
		if as, ok := c.assignments(); ok {
			pc := newPubContext(cmds, i, dir, cd)
			pc.vars = vars
			sequential := c.sep == ";" || c.sep == "&&" || c.sep == "\n"
			next := shellVars{}
			maps.Copy(next, vars)
			for _, a := range as {
				if !a.ok {
					continue
				}
				v, ok := pc.evalWord(a.value, a.exps, 0)
				if ok && sequential {
					next[a.name] = v
				} else {
					delete(next, a.name)
				}
			}
			vars = next
		}
		switch c.name() {
		case "cd":
			if len(c.words) == 2 && !c.wordDyn[1] && c.words[1] != "-" && (!strings.HasPrefix(c.words[1], "~") || c.words[1] == "~" || strings.HasPrefix(c.words[1], "~/")) {
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
		pc.vars = vars
		if kinds[i] == pubPush {
			push, err := pc.push(s.pushIsPrivate)
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
		if !private {
			p.scanLine = true
		}
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
	// vars are the variables the line sets before the publication.
	vars shellVars
}

func newPubContext(line []command, i int, dir string, cd bool) pubContext {
	pc := pubContext{
		cmd:  line[i],
		line: line,
		dir:  dir,
		cd:   cd,
	}
	pc.writes = append(pc.writes, line[i].writes...)
	for _, j := range alongside(line, i) {
		all := append([]command{line[j]}, flatten(line[j], 0)...)
		for k := range all {
			pc.writes = append(pc.writes, all[k].writes...)
			if pc.changer == "" && !inert(all[k]) {
				pc.changer = all[k].String()
			}
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

// readStdin returns what a publication reads on stdin.
func (pc pubContext) readStdin() (string, error) {
	c := pc.cmd
	sources := c.stdinSources
	if c.pipeFrom >= 0 {
		sources++
	}
	switch {
	case sources == 0:
		return "", uncheckable("it posts stdin, but the command gives it nothing the check can read")
	case sources > 1:
		return "", uncheckable("it posts stdin, which more than one pipe or redirection feeds")
	case c.hasStdin && c.stdinDynamic:
		return "", uncheckable("it posts a here-document that expands variables or commands; quote its delimiter (<<'EOF')")
	case c.hasStdin:
		return c.stdin, nil
	case c.stdinFile != "" && c.stdinFileDynamic:
		return "", uncheckable("it posts stdin from a file named by a variable")
	case c.stdinFile != "":
		return pc.readFile(c.stdinFile)
	}
	src := pc.line[c.pipeFrom]
	words, ok := pc.evalWords(src, 0)
	switch {
	case !ok || src.stdinSources > 0 || src.pipeFrom >= 0 || len(src.assigns) > 0:
	case src.name() == "printf" && len(words) >= 3 && words[1] == "%s":
		return strings.Join(words[2:], ""), nil
	case src.name() == "echo" && plainEcho(words[1:]):
		return strings.Join(words[1:], " ") + "\n", nil
	}
	return "", uncheckable("it posts stdin from `%s`, whose output the check cannot work out", src)
}

// plainEcho reports whether echo prints args as they are: no option and no
// backslash, which some shells' echo interprets.
func plainEcho(args []string) bool {
	for _, a := range args {
		if strings.HasPrefix(a, "-") || strings.Contains(a, `\`) {
			return false
		}
	}
	return true
}

// push returns the git push of a publication, refusing forms whose target
// the check cannot work out.
func (pc pubContext) push(isPrivate func(pushSpec) bool) (pushSpec, error) {
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
	spec := pushSpec{
		dir:  dir,
		args: c.words[i+1:],
	}
	// A push to a private repository is not this check's concern, even
	// after a command that changes what it pushes.
	if pc.changer != "" && !isPrivate(spec) {
		return pushSpec{}, uncheckable("`%s` runs before the push or alongside it and may change what it pushes; run the push in its own call", pc.changer)
	}
	return spec, nil
}

// pushIsPrivate reports whether a push goes to a private Runware repository.
func (s *Scanner) pushIsPrivate(p pushSpec) bool {
	ctx := context.Background()
	g := Git{
		Dir: p.dir,
	}
	remote := ""
	for i := 0; i < len(p.args) && remote == ""; i++ {
		a := p.args[i]
		switch {
		case a == "-o" || a == "--push-option" || a == "--repo" || a == "--receive-pack" || a == "--exec":
			i++
		case strings.HasPrefix(a, "-"):
		default:
			remote = a
		}
	}
	if remote == "" {
		remote = "origin"
		if current := g.CurrentBranch(ctx); current != "" {
			if out, err := g.run(ctx, "config", "branch."+current+".remote"); err == nil && strings.TrimSpace(out) != "" {
				remote = strings.TrimSpace(out)
			}
		}
	}
	return s.remoteIsPrivate(ctx, g, remote)
}

// ghTexts returns what a gh write publishes, and whether its destination is
// a private Runware repository, where it publishes nothing to check.
func (s *Scanner) ghTexts(pc pubContext, repoOf repoResolver) ([]publication, bool, error) {
	c := pc.cmd
	if anyTrue(c.assignDyn) {
		return nil, false, uncheckable("`%s` sets a variable from an expansion", c)
	}
	words, ok := pc.evalWords(c, 0)
	if !ok {
		return nil, false, uncheckable("`%s` uses a variable set outside the command, a glob, or a substitution the check cannot work out", c)
	}
	c.words = words
	c.wordDyn = make([]bool, len(words))
	pc.cmd = c
	repos := pc.defaultRepos(repoOf)
	group := parseFlags(c.words[1:], c.wordDyn[1:], []flagSpec{flagRepo}).pos[0]
	if group == wordAPI {
		return s.ghAPITexts(pc, repos)
	}
	if len(parseFlags(c.words[1:], c.wordDyn[1:], []flagSpec{flagRepo}).pos) < 2 {
		return nil, false, uncheckable("`%s` is not a gh command the check knows", c)
	}
	p := parseFlags(c.words[1:], c.wordDyn[1:], []flagSpec{flagRepo})
	name := p.pos[0] + " " + p.pos[1]
	p = parseFlags(c.words[1:], c.wordDyn[1:], append([]flagSpec{flagRepo}, ghWriteSpecs[name]...))
	var texts []publication
	for _, f := range p.flags {
		label := "gh " + name + " " + strings.ReplaceAll(f.spec.long, "-", " ")
		switch f.spec.kind {
		case flagText:
			texts = append(texts, publication{
				label: label,
				text:  f.value,
			})
		case flagFile:
			text, err := pc.readFile(f.value)
			if err != nil {
				return nil, false, err
			}
			texts = append(texts, publication{
				label: label,
				text:  text,
			})
		case flagValue:
			if f.spec.long == flagRepo.long {
				repos = []string{f.value}
			}
		}
	}
	// A pull request or issue URL names the destination, whatever -R says.
	// Only the commands that take one: create has no positional selector.
	if p.pos[1] != "create" && p.pos[0] != wordRelease {
		args := p.pos[2:]
		if len(args) > 0 {
			var dests []string
			for _, a := range args {
				if r := ghURLRepo(a); r != "" {
					dests = append(dests, r)
				} else {
					dests = append(dests, repos...)
				}
			}
			repos = dests
		}
	}
	if s.allPrivate(repos) {
		return nil, true, nil
	}
	return texts, false, nil
}

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

// apiField is one -f or -F field of a gh api call.
type apiField struct {
	key   string
	value string
}

var apiRepoPath = regexp.MustCompile(`^/?repos/([^/]+)/([^/]+)`)

// apiTextFields are the fields of a REST call that publish text: a comment,
// review, issue or pull request body and title, and the commit title and
// message of a merge or of a file change.
var apiTextFields = map[string]struct{}{
	"body":           {},
	"commit_message": {},
	"commit_title":   {},
	"message":        {},
	"title":          {},
}

func (s *Scanner) ghAPITexts(pc pubContext, repos []string) ([]publication, bool, error) {
	c := pc.cmd
	p := parseFlags(c.words[1:], c.wordDyn[1:], apiSpecs)
	endpoint := ""
	if len(p.pos) > 1 {
		endpoint = p.pos[1]
	}
	var fields []apiField
	var inputs []string
	for _, f := range p.flags {
		switch f.spec.long {
		case "raw-field", "field":
			k, v, _ := strings.Cut(f.value, "=")
			if file, ok := strings.CutPrefix(v, "@"); ok && f.spec.long == "field" {
				text, err := pc.readFile(file)
				if err != nil {
					return nil, false, err
				}
				v = text
			}
			fields = append(fields, apiField{
				key:   k,
				value: v,
			})
		case "input":
			text, err := pc.readFile(f.value)
			if err != nil {
				return nil, false, err
			}
			inputs = append(inputs, text)
		}
	}
	graphqlMutation := false
	for _, f := range fields {
		if f.key == "query" && strings.Contains(f.value, "mutation") {
			graphqlMutation = true
		}
	}
	var texts []publication
	for _, text := range inputs {
		texts = append(texts, inputTexts(text, endpoint)...)
	}
	for _, f := range fields {
		_, isText := apiTextFields[fieldLeaf(f.key)]
		if isText || graphqlMutation {
			texts = append(texts, publication{
				label: "gh api field " + f.key,
				text:  f.value,
			})
		}
	}
	if m := apiRepoPath.FindStringSubmatch(endpoint); m != nil {
		repo := m[1] + "/" + m[2]
		if strings.Contains(repo, "{") {
			if s.allPrivate(repos) {
				return nil, true, nil
			}
		} else if s.isPrivateTarget(repo) {
			return nil, true, nil
		}
	}
	return texts, false, nil
}

// fieldLeaf returns the last key of a gh api field name: "body" for
// "comments[][body]", "labels" for "labels[]".
func fieldLeaf(key string) string {
	parts := strings.FieldsFunc(key, func(r rune) bool {
		return r == '[' || r == ']'
	})
	if len(parts) == 0 {
		return key
	}
	return parts[len(parts)-1]
}

// inputTexts returns the text a JSON request body passed with --input would
// publish, or the whole text when it is not JSON: every string under a key in
// apiTextFields at any depth (a review's comments[].body, for example), and
// for a GraphQL mutation the query and every string in its variables.
func inputTexts(text, endpoint string) []publication {
	var v any
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		return []publication{{
			label: "gh api --input",
			text:  text,
		}}
	}
	var out []publication
	if obj, ok := v.(map[string]any); ok {
		query, _ := obj["query"].(string)
		isGraphQL := strings.TrimPrefix(endpoint, "/") == "graphql" || query != ""
		if isGraphQL && strings.Contains(query, "mutation") {
			out = append(out, publication{
				label: "gh api --input query",
				text:  query,
			})
			apiInputWalk.collect(obj["variables"], "variables", true, &out)
			delete(obj, "query")
			delete(obj, "variables")
		}
	}
	apiInputWalk.collect(v, "", false, &out)
	return out
}

// stringWalk collects the strings of a decoded JSON value that would be
// published: those under one of keys, labeled with label and their path.
type stringWalk struct {
	label string
	keys  map[string]struct{}
}

var apiInputWalk = stringWalk{
	label: "gh api --input",
	keys:  apiTextFields,
}

// collect walks v in a stable order and appends the strings to publish: every
// string when all is set, otherwise only those under a key in w.keys. path
// names the value in labels.
func (w stringWalk) collect(v any, path string, all bool, out *[]publication) {
	switch x := v.(type) {
	case string:
		if all {
			*out = append(*out, publication{
				label: w.label + " " + path,
				text:  x,
			})
		}
	case []any:
		for i, e := range x {
			w.collect(e, fmt.Sprintf("%s[%d]", path, i), all, out)
		}
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		for _, k := range keys {
			p := k
			if path != "" {
				p = path + "." + k
			}
			_, isText := w.keys[k]
			w.collect(x[k], p, all || isText, out)
		}
	}
}

// pushTag is a tag a git push would publish.
type pushTag struct {
	name   string
	object string
}

// pushTargets is what a git push would publish.
type pushTargets struct {
	remote string
	ranges []Range
	tags   []pushTag
}

// pushTargets returns what a git push would publish: for each pushed branch,
// what it has beyond its remote-tracking branch, or beyond its merge-base with
// main when the remote does not have it yet; and each pushed tag, named
// directly, as refs/tags/NAME, as `tag NAME`, or with --tags or --follow-tags.
func (s *Scanner) pushTargets(ctx context.Context, p pushSpec) (pushTargets, error) {
	g := Git{
		Dir: p.dir,
	}
	var positional []string
	all := false
	allTags := false
	followTags := false
	for i := 0; i < len(p.args); i++ {
		a := p.args[i]
		switch {
		case a == "--all" || a == "--branches" || a == "--mirror":
			all = true
		case a == "--tags":
			allTags = true
		case a == "--follow-tags":
			followTags = true
		case a == "--no-follow-tags":
			followTags = false
		case a == "-o" || a == "--push-option" || a == "--repo" || a == "--receive-pack" || a == "--exec":
			i++
		case strings.HasPrefix(a, "-"):
		default:
			positional = append(positional, a)
		}
	}

	current := g.CurrentBranch(ctx)
	t := pushTargets{
		remote: "origin",
	}
	if current != "" {
		if out, err := g.run(ctx, "config", "branch."+current+".remote"); err == nil && strings.TrimSpace(out) != "" {
			t.remote = strings.TrimSpace(out)
		}
	}
	var refspecs []string
	if len(positional) > 0 {
		t.remote = positional[0]
		for i := 1; i < len(positional); i++ {
			if positional[i] == wordTag && i+1 < len(positional) {
				i++
				refspecs = append(refspecs, "refs/tags/"+positional[i])
				continue
			}
			refspecs = append(refspecs, positional[i])
		}
	}
	if s.remoteIsPrivate(ctx, g, t.remote) {
		return pushTargets{}, nil
	}
	if all {
		out, err := g.run(ctx, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
		if err != nil {
			return pushTargets{}, err
		}
		refspecs = strings.Fields(out)
	}
	// git push --tags alone pushes only the tags.
	if len(refspecs) == 0 && current != "" && !allTags {
		refspecs = []string{current}
	}
	if !slices.Contains(p.args, "--no-follow-tags") {
		if out, err := g.run(ctx, "config", "--bool", "push.followTags"); err == nil && strings.TrimSpace(out) == "true" {
			followTags = true
		}
	}
	if allTags {
		out, err := g.run(ctx, "for-each-ref", "--format=%(refname)", "refs/tags/")
		if err != nil {
			return pushTargets{}, err
		}
		refspecs = append(refspecs, strings.Fields(out)...)
	}

	for _, spec := range refspecs {
		spec = strings.TrimPrefix(spec, "+")
		src, dst, hasDst := strings.Cut(spec, ":")
		if src == "" {
			continue // deleting a remote ref publishes nothing
		}
		if tag, ok := g.tagName(ctx, src, dst, hasDst); ok {
			object, err := g.run(ctx, "rev-parse", "--verify", "--quiet", src)
			if err != nil {
				continue // not a tag here: git push will say
			}
			t.tags = append(t.tags, pushTag{
				name:   tag,
				object: strings.TrimSpace(object),
			})
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
		tracking, _ := g.revParse(ctx, "refs/remotes/"+t.remote+"/"+dst)
		base, err := g.PushBase(ctx, t.remote, tracking, head)
		if err != nil {
			return pushTargets{}, err
		}
		t.ranges = append(t.ranges, Range{
			Base:   base,
			Head:   head,
			Branch: dst,
			Remote: t.remote,
		})
	}
	if followTags && !allTags {
		tags, err := g.followTags(ctx, t.ranges)
		if err != nil {
			return pushTargets{}, err
		}
		for _, tag := range tags {
			if !slices.ContainsFunc(t.tags, func(x pushTag) bool { return x.name == tag.name }) {
				t.tags = append(t.tags, tag)
			}
		}
	}
	return t, nil
}

// followTags returns the tags git push --follow-tags adds to a push: the
// annotated tags that point at a commit reachable from a pushed branch.
// Lightweight tags, and tags on commits that are not pushed, stay behind.
func (g Git) followTags(ctx context.Context, ranges []Range) ([]pushTag, error) {
	var out []pushTag
	seen := map[string]struct{}{}
	for _, r := range ranges {
		list, err := g.run(ctx, "for-each-ref", "--merged="+r.Head, "--format=%(objecttype) %(objectname) %(refname)", "refs/tags/")
		if err != nil {
			return nil, err
		}
		for l := range strings.SplitSeq(strings.TrimSpace(list), "\n") {
			fields := strings.Fields(l)
			if len(fields) != 3 || fields[0] != wordTag {
				continue
			}
			name := strings.TrimPrefix(fields[2], "refs/tags/")
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, pushTag{
				name:   name,
				object: fields[1],
			})
		}
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
		t, err := s.pushTargets(ctx, push)
		if err != nil {
			return out, err
		}
		g := Git{
			Dir: push.dir,
		}
		for _, r := range t.ranges {
			f, err := s.ScanRange(ctx, g, r)
			if err != nil {
				return out, err
			}
			out = append(out, f...)
		}
		for _, tag := range t.tags {
			f, err := s.ScanTag(ctx, g, t.remote, tag.name, tag.object)
			if err != nil {
				return out, err
			}
			out = append(out, f...)
		}
	}
	return out, nil
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
