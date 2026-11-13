package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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
}

// repoResolver returns the owner/name of the GitHub repository a directory
// belongs to, or "" when it cannot tell.
type repoResolver func(dir string) string

// planCommand finds the git pushes and the GitHub titles, bodies and comments
// in a Bash command line run from dir.
func (s *Scanner) planCommand(command, dir string, repoOf repoResolver) plan {
	var p plan
	for _, c := range parseShell(command, dir) {
		args := stripPrefixes(c.args)
		if len(args) == 0 {
			continue
		}
		switch args[0] {
		case "cd":
			if len(args) > 1 {
				dir = resolvePath(dir, args[1])
			}
		case "git":
			if push, ok := gitPush(args, dir); ok {
				p.pushes = append(p.pushes, push)
			}
		case "gh":
			p.texts = append(p.texts, s.ghTexts(args, c, dir, repoOf)...)
		}
	}
	return p
}

var envAssignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// stripPrefixes drops leading variable assignments and wrapper commands.
func stripPrefixes(args []string) []string {
	for len(args) > 0 {
		switch {
		case envAssignment.MatchString(args[0]):
			args = args[1:]
		case args[0] == "env" || args[0] == "command" || args[0] == "time" || args[0] == "nohup" || args[0] == "exec":
			args = args[1:]
		default:
			return args
		}
	}
	return args
}

// gitPush recognizes `git [global options] push ...`.
func gitPush(args []string, dir string) (pushSpec, bool) {
	i := 1
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		switch args[i] {
		case "-C":
			if i+1 < len(args) {
				dir = resolvePath(dir, args[i+1])
			}
			i += 2
		case "-c", "--git-dir", "--work-tree", "--namespace", "--exec-path":
			i += 2
		default:
			i++
		}
	}
	if i >= len(args) || args[i] != "push" {
		return pushSpec{}, false
	}
	return pushSpec{
		dir:  dir,
		args: args[i+1:],
	}, true
}

// ghWriteCommands are the gh subcommands that publish a title, body or comment.
var ghWriteCommands = map[string]struct{}{
	"pr create":     {},
	"pr edit":       {},
	"pr comment":    {},
	"pr review":     {},
	"issue create":  {},
	"issue edit":    {},
	"issue comment": {},
}

func (s *Scanner) ghTexts(args []string, c shellCmd, dir string, repoOf repoResolver) []publication {
	if len(args) < 2 {
		return nil
	}
	if args[1] == "api" {
		return s.ghAPITexts(args[2:], c, dir, repoOf)
	}
	if len(args) < 3 {
		return nil
	}
	name := args[1] + " " + args[2]
	if _, ok := ghWriteCommands[name]; !ok {
		return nil
	}
	var texts []publication
	repo := ""
	rest := args[3:]
	for i := 0; i < len(rest); i++ {
		flag, value, hasValue := strings.Cut(rest[i], "=")
		if !strings.HasPrefix(flag, "--") {
			flag, hasValue = rest[i], false
		}
		takeValue := func() string {
			if hasValue {
				return value
			}
			if i+1 < len(rest) {
				i++
				return rest[i]
			}
			return ""
		}
		switch flag {
		case "-t", "--title":
			texts = append(texts, publication{
				label: "gh " + name + " title",
				text:  takeValue(),
			})
		case "-b", "--body":
			texts = append(texts, publication{
				label: "gh " + name + " body",
				text:  takeValue(),
			})
		case "-F", "--body-file":
			if text, ok := readArgFile(takeValue(), c, dir); ok {
				texts = append(texts, publication{
					label: "gh " + name + " body file",
					text:  text,
				})
			}
		case "-R", "--repo":
			repo = takeValue()
		}
	}
	if repo == "" {
		repo = repoOf(dir)
	}
	if s.isPrivateTarget(repo) {
		return nil
	}
	return texts
}

// readArgFile reads a file argument; "-" is the command's stdin.
func readArgFile(name string, c shellCmd, dir string) (string, bool) {
	if name == "-" {
		if c.hasStdin {
			return c.stdin, true
		}
		if c.stdinFile == "" {
			return "", false
		}
		name = c.stdinFile
	}
	b, err := os.ReadFile(resolvePath(dir, name))
	if err != nil {
		return "", false
	}
	return string(b), true
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

// apiTextFields are the fields of a REST call that publish text.
var apiTextFields = map[string]struct{}{
	"body":  {},
	"title": {},
}

func (s *Scanner) ghAPITexts(args []string, c shellCmd, dir string, repoOf repoResolver) []publication {
	var texts []publication
	endpoint := ""
	graphqlMutation := false
	var fields []apiField
	var inputs []string
	for i := 0; i < len(args); i++ {
		flag, value, hasValue := strings.Cut(args[i], "=")
		if !strings.HasPrefix(flag, "--") {
			flag, hasValue = args[i], false
		}
		takeValue := func() string {
			if hasValue {
				return value
			}
			if i+1 < len(args) {
				i++
				return args[i]
			}
			return ""
		}
		switch flag {
		case "-f", "--raw-field":
			k, v, _ := strings.Cut(takeValue(), "=")
			fields = append(fields, apiField{
				key:   k,
				value: v,
			})
		case "-F", "--field":
			k, v, _ := strings.Cut(takeValue(), "=")
			if file, ok := strings.CutPrefix(v, "@"); ok {
				if text, ok := readArgFile(file, c, dir); ok {
					v = text
				}
			}
			fields = append(fields, apiField{
				key:   k,
				value: v,
			})
		case "--input":
			if text, ok := readArgFile(takeValue(), c, dir); ok {
				inputs = append(inputs, text)
			}
		case "-X", "--method", "-H", "--header", "-q", "--jq", "-t", "--template", "--hostname", "-p", "--preview", "--cache":
			takeValue()
		default:
			if !strings.HasPrefix(args[i], "-") && endpoint == "" {
				endpoint = args[i]
			}
		}
	}
	for _, f := range fields {
		if f.key == "query" && strings.Contains(f.value, "mutation") {
			graphqlMutation = true
		}
	}
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
	if len(texts) == 0 {
		return nil
	}
	if m := apiRepoPath.FindStringSubmatch(endpoint); m != nil {
		repo := m[1] + "/" + m[2]
		if strings.Contains(repo, "{") {
			repo = repoOf(dir)
		}
		if s.isPrivateTarget(repo) {
			return nil
		}
	}
	return texts
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
	for i := 0; i < len(p.args); i++ {
		a := p.args[i]
		switch {
		case a == "--all" || a == "--branches" || a == "--mirror":
			all = true
		case a == "--tags" || a == "--follow-tags":
			allTags = true
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
			if positional[i] == "tag" && i+1 < len(positional) {
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
	// git push --tags alone pushes only the tags; --follow-tags also pushes
	// the current branch.
	onlyTags := allTags && slices.Contains(p.args, "--tags")
	if len(refspecs) == 0 && current != "" && !onlyTags {
		refspecs = []string{current}
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
		var base string
		if tracking, err := g.revParse(ctx, "refs/remotes/"+t.remote+"/"+dst); err == nil {
			base, err = g.MergeBase(ctx, tracking, head)
			if err != nil {
				return pushTargets{}, err
			}
		} else {
			base, err = g.DefaultBase(ctx, t.remote, head)
			if err != nil {
				return pushTargets{}, err
			}
		}
		t.ranges = append(t.ranges, Range{
			Base:   base,
			Head:   head,
			Branch: dst,
		})
	}
	return t, nil
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
	p := s.planCommand(bash.Command, dir, repoOf)
	var out []Finding
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
