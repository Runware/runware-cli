package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// The Bash hook fails closed. A command line that runs a GitHub write or a
// git push anywhere, including inside a substitution, a subshell, a `bash -c`
// string or xargs, must run it in a simple form the hook reads completely:
// a top-level gh or git command with literal arguments, files that exist and
// that nothing in the same line changes, and stdin from one known source.
// Anything else is blocked with UncheckableError.

// UncheckableError reports a command line that would or could publish to
// GitHub in a form the hook cannot read completely.
type UncheckableError struct {
	Reason string
}

func (e *UncheckableError) Error() string {
	return e.Reason
}

func uncheckable(format string, a ...any) error {
	return &UncheckableError{
		Reason: fmt.Sprintf(format, a...),
	}
}

// maxNesting bounds the recursion into substitutions and `bash -c` strings.
// A deeper line is treated as publishing.
const maxNesting = 8

// command is a simple command with its leading assignments and wrappers
// removed.
type command struct {
	shellCmd
	words      []string
	wordDyn    []bool
	wordExps   [][]expansion
	assigns    []string
	assignDyn  []bool
	assignExps [][]expansion
}

var envAssignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\+?=`)

// simpleWrappers run their arguments as a command without changing it.
var simpleWrappers = map[string]struct{}{
	"command": {},
	"env":     {},
	"exec":    {},
	"nohup":   {},
	"time":    {},
}

func newCommand(c shellCmd) command {
	cmd := command{
		shellCmd: c,
	}
	i := 0
	for ; i < len(c.args); i++ {
		if envAssignment.MatchString(c.args[i]) {
			cmd.assigns = append(cmd.assigns, c.args[i])
			cmd.assignDyn = append(cmd.assignDyn, c.dyn[i])
			cmd.assignExps = append(cmd.assignExps, c.exps[i])
			continue
		}
		if _, ok := simpleWrappers[c.args[i]]; ok && !c.dyn[i] {
			continue
		}
		break
	}
	cmd.words = c.args[i:]
	cmd.wordDyn = c.dyn[i:]
	cmd.wordExps = c.exps[i:]
	return cmd
}

func commands(line shellLine) []command {
	out := make([]command, len(line.cmds))
	for i := range line.cmds {
		out[i] = newCommand(line.cmds[i])
	}
	return out
}

// name returns the base name of the command, or "".
func (c command) name() string {
	if len(c.words) == 0 {
		return ""
	}
	return filepath.Base(c.words[0])
}

// String returns the command as it reads in a message.
func (c command) String() string {
	s := strings.Join(c.words, " ")
	if len(s) > 60 {
		s = s[:57] + "..."
	}
	return s
}

// pubKind says whether a command publishes to GitHub.
type pubKind int

const (
	pubNone pubKind = iota
	pubGH
	pubPush
	// pubUnknown is a command that may publish but cannot be told apart:
	// a command name in a variable, a gh alias or extension.
	pubUnknown
)

// writeWords are the words that make a command with a dynamic name look
// like a GitHub write or a git push.
var writeWords = map[string]struct{}{
	wordAPI:     {},
	"close":     {},
	"comment":   {},
	"create":    {},
	"edit":      {},
	"gh":        {},
	"git":       {},
	wordIssue:   {},
	"merge":     {},
	"pr":        {},
	wordPush:    {},
	wordRelease: {},
	"reopen":    {},
	"review":    {},
}

// publishes reports whether words, a command and its arguments, publish.
func publishes(words []string, dyn []bool) pubKind {
	if len(words) == 0 {
		return pubNone
	}
	if dyn[0] {
		for _, w := range words[1:] {
			if _, ok := writeWords[filepath.Base(w)]; ok {
				return pubUnknown
			}
		}
		return pubNone
	}
	switch filepath.Base(words[0]) {
	case "gh":
		return ghPublishes(words, dyn)
	case "git":
		return gitPublishes(words, dyn)
	}
	return pubNone
}

// gitValueOptions are the git options before the subcommand that take a value.
var gitValueOptions = map[string]struct{}{
	"-C":          {},
	"-c":          {},
	"--exec-path": {},
	"--git-dir":   {},
	"--namespace": {},
	"--work-tree": {},
}

// gitSubcommand returns the index of git's subcommand in words, or len(words).
func gitSubcommand(words []string) int {
	i := 1
	for i < len(words) && strings.HasPrefix(words[i], "-") {
		if _, ok := gitValueOptions[words[i]]; ok {
			i++
		}
		i++
	}
	return i
}

func gitPublishes(words []string, dyn []bool) pubKind {
	i := gitSubcommand(words)
	if i >= len(words) {
		return pubNone
	}
	if anyTrue(dyn[1 : i+1]) {
		return pubUnknown
	}
	if words[i] == wordPush {
		return pubPush
	}
	return pubNone
}

// shells run a command line given as an argument or on stdin.
var shells = map[string]struct{}{
	"bash": {},
	"dash": {},
	"fish": {},
	"ksh":  {},
	"sh":   {},
	"zsh":  {},
}

// evaluators run their arguments as a command line.
var evaluators = map[string]struct{}{
	"alias": {},
	"eval":  {},
	"trap":  {},
	"watch": {},
}

// scriptInterpreters run code in another language, which the hook only
// searches for gh and push.
var scriptInterpreters = map[string]struct{}{
	"bun":       {},
	"deno":      {},
	"node":      {},
	"osascript": {},
	"perl":      {},
	"php":       {},
	"python":    {},
	"python3":   {},
	"ruby":      {},
}

var publicationMention = regexp.MustCompile(`\bgh\b|\bpush\b`)

// mentionsPublication reports whether text that runs as code names gh or push.
func mentionsPublication(text string) bool {
	return publicationMention.MatchString(text)
}

// containsPublication reports whether a command line run in some other way
// (a substitution, a `bash -c` string) publishes anywhere.
func containsPublication(src string, depth int) bool {
	if depth > maxNesting {
		return true
	}
	cmds := commands(parseShell(src))
	for i := range cmds {
		if publishes(cmds[i].words, cmds[i].wordDyn) != pubNone || hiddenPublication(cmds[i], cmds, depth) != "" {
			return true
		}
	}
	return false
}

// hiddenPublication returns where c runs a publication in a way the hook
// cannot follow, or "": in a substitution, through a wrapper such as xargs,
// or in a string or script that a shell or interpreter runs.
func hiddenPublication(c command, line []command, depth int) string {
	for _, src := range c.nested {
		if containsPublication(src, depth+1) {
			return "inside a command substitution"
		}
	}
	if slices.ContainsFunc(c.opaque, mentionsPublication) {
		return "inside a here-document that runs commands"
	}
	for k, w := range c.words {
		base := filepath.Base(w)
		if k > 0 && publishes(c.words[k:], c.wordDyn[k:]) != pubNone {
			return fmt.Sprintf("through `%s`", c.words[0])
		}
		if _, ok := shells[base]; ok {
			for _, a := range c.words[k+1:] {
				if containsPublication(a, depth+1) {
					return fmt.Sprintf("inside a `%s` command string", base)
				}
			}
			if c.hasStdin && containsPublication(c.stdin, depth+1) {
				return fmt.Sprintf("in a script that `%s` reads", base)
			}
			if c.pipeFrom >= 0 && c.pipeFrom < len(line) && mentionsPublication(strings.Join(line[c.pipeFrom].args, " ")) {
				return fmt.Sprintf("in a script piped to `%s`", base)
			}
		}
		if _, ok := evaluators[base]; ok && containsPublication(strings.Join(c.words[k+1:], " "), depth+1) {
			return fmt.Sprintf("inside `%s`", base)
		}
		if _, ok := scriptInterpreters[base]; ok && mentionsPublication(strings.Join(c.words[k+1:], " ")+"\n"+c.stdin) {
			return fmt.Sprintf("inside a `%s` script", base)
		}
	}
	return ""
}

// shellKeywords start or end a compound command, whose body the shell runs
// later, repeatedly or not at all.
var shellKeywords = map[string]struct{}{
	"!":        {},
	"case":     {},
	"do":       {},
	"done":     {},
	"elif":     {},
	"else":     {},
	"esac":     {},
	"fi":       {},
	"for":      {},
	"function": {},
	"if":       {},
	"select":   {},
	"then":     {},
	"until":    {},
	"while":    {},
	"{":        {},
	"}":        {},
}

// flatten returns the commands that the substitutions in c run, at any depth.
func flatten(c command, depth int) []command {
	if depth > maxNesting {
		return nil
	}
	var out []command
	for _, src := range c.nested {
		nested := commands(parseShell(src))
		for i := range nested {
			out = append(out, nested[i])
			out = append(out, flatten(nested[i], depth+1)...)
		}
	}
	return out
}

// alongside returns the indexes of the commands of a line that run before
// cmds[i] or at the same time: every earlier command, the rest of its
// pipeline, and every later command when its pipeline runs in the background.
func alongside(cmds []command, i int) []int {
	var out []int
	for j := range i {
		out = append(out, j)
	}
	end := i
	for end < len(cmds)-1 && (cmds[end].sep == "|" || cmds[end].sep == "|&") {
		end++
	}
	if cmds[end].sep == "&" {
		end = len(cmds) - 1
	}
	for j := i + 1; j <= end; j++ {
		out = append(out, j)
	}
	return out
}

// sensitiveEnv are the variables that change where gh or git publishes.
var sensitiveEnv = map[string]struct{}{
	"GH_HOST":       {},
	"GH_REPO":       {},
	"GIT_DIR":       {},
	"GIT_NAMESPACE": {},
	"GIT_WORK_TREE": {},
}

func setsSensitive(assigns []string) bool {
	for _, a := range assigns {
		name, _, _ := strings.Cut(strings.Replace(a, "+=", "=", 1), "=")
		if _, ok := sensitiveEnv[name]; ok {
			return true
		}
	}
	return false
}

// inertCommands neither write files nor change git refs.
var inertCommands = map[string]struct{}{
	":":        {},
	"[":        {},
	"[[":       {},
	"basename": {},
	"cat":      {},
	"date":     {},
	"df":       {},
	"diff":     {},
	"dirname":  {},
	"du":       {},
	"echo":     {},
	"egrep":    {},
	"false":    {},
	"fgrep":    {},
	"file":     {},
	"grep":     {},
	"head":     {},
	"jq":       {},
	"ls":       {},
	"printenv": {},
	"printf":   {},
	"pwd":      {},
	"readlink": {},
	"realpath": {},
	"rg":       {},
	"sleep":    {},
	"stat":     {},
	"tail":     {},
	"test":     {},
	"true":     {},
	"type":     {},
	"uname":    {},
	"wc":       {},
	"which":    {},
	"whoami":   {},
}

// declarations set variables, which inert allows unless they are sensitive.
var declarations = map[string]struct{}{
	"declare":  {},
	"export":   {},
	"local":    {},
	"readonly": {},
	"typeset":  {},
}

// inert reports whether c can run before a publication without changing
// what it publishes: it writes no file (apart from its redirections, which
// are checked on their own) and changes no git ref or gh destination.
func inert(c command) bool {
	if setsSensitive(c.assigns) {
		return false
	}
	if len(c.words) == 0 {
		return true
	}
	if c.wordDyn[0] {
		return false
	}
	name := c.name()
	if _, ok := declarations[name]; ok {
		return !setsSensitive(c.words[1:])
	}
	switch name {
	case "cd":
		return true // the caller follows it
	case "git":
		return gitInert(c.words, c.wordDyn)
	case "gh":
		return ghInert(c.words, c.wordDyn)
	}
	_, ok := inertCommands[name]
	return ok
}

// gitReadSubcommands change no file in the working tree and no ref a push
// publishes.
var gitReadSubcommands = map[string]struct{}{
	"add":           {},
	"blame":         {},
	"cat-file":      {},
	"count-objects": {},
	"describe":      {},
	"diff":          {},
	"fetch":         {},
	"for-each-ref":  {},
	"grep":          {},
	"log":           {},
	"ls-files":      {},
	"ls-remote":     {},
	"merge-base":    {},
	wordPush:        {},
	"rev-list":      {},
	"rev-parse":     {},
	"shortlog":      {},
	"show":          {},
	"show-ref":      {},
	"status":        {},
	"version":       {},
}

// gitListOptions are the options that keep branch, tag and remote listing.
var gitListOptions = map[string]struct{}{
	"--all":          {},
	"--list":         {},
	"--remotes":      {},
	"--show-current": {},
	"--verbose":      {},
	"-a":             {},
	"-l":             {},
	"-r":             {},
	"-v":             {},
	"-vv":            {},
}

func gitInert(words []string, dyn []bool) bool {
	i := 1
	for i < len(words) && strings.HasPrefix(words[i], "-") {
		switch words[i] {
		case "-C":
			i += 2
		case "--no-pager", "--no-optional-locks":
			i++
		default:
			return false
		}
	}
	if i >= len(words) {
		return true
	}
	if anyTrue(dyn[:i+1]) {
		return false
	}
	sub, rest := words[i], words[i+1:]
	if _, ok := gitReadSubcommands[sub]; ok {
		return true
	}
	switch sub {
	case "branch", wordTag:
		for _, a := range rest {
			if _, ok := gitListOptions[a]; !ok {
				return false
			}
		}
		return true
	case "remote":
		return len(rest) == 0 || rest[0] == "-v" || rest[0] == "get-url" || rest[0] == "show"
	case "config":
		return len(rest) > 0 && (rest[0] == "--get" || rest[0] == "--get-all" || rest[0] == "--list" || rest[0] == "-l")
	}
	return false
}

// shellVars are the variables a command line sets before a publication,
// with the values the check worked out from the line itself. A name set to
// something it could not work out is absent, like one set outside the line.
type shellVars map[string]string

// assignment is NAME=value split, with the expansions of value.
type assignment struct {
	name  string
	value string
	exps  []expansion
	ok    bool
}

func splitAssignment(word string, exps []expansion) assignment {
	name, value, ok := strings.Cut(word, "=")
	if !ok || strings.HasSuffix(name, "+") || !shellName.MatchString(name) {
		return assignment{}
	}
	shift := len(name) + 1
	shifted := make([]expansion, 0, len(exps))
	for _, e := range exps {
		if e.start < shift {
			return assignment{}
		}
		e.start -= shift
		e.end -= shift
		// An assignment's value is not split into words.
		e.quoted = true
		shifted = append(shifted, e)
	}
	return assignment{
		name:  name,
		value: value,
		exps:  shifted,
		ok:    true,
	}
}

// assignments returns what a command that only sets variables sets: plain
// NAME=value words, or export NAME=value. ok is false for any other command.
func (c command) assignments() ([]assignment, bool) {
	var out []assignment
	switch {
	case len(c.words) == 0:
		for i, a := range c.assigns {
			out = append(out, splitAssignment(a, c.assignExps[i]))
		}
	case c.name() == "export" && !c.wordDyn[0]:
		for i, w := range c.words[1:] {
			if strings.HasPrefix(w, "-") {
				return nil, false
			}
			if !strings.Contains(w, "=") {
				continue
			}
			out = append(out, splitAssignment(w, c.wordExps[i+1]))
		}
	default:
		return nil, false
	}
	return out, true
}

// evalWord returns the value of a word whose expansions all come from the
// command line itself, or false.
func (pc pubContext) evalWord(word string, exps []expansion, depth int) (string, bool) {
	var b strings.Builder
	last := 0
	for _, e := range exps {
		var v string
		switch e.kind {
		case expVar:
			val, ok := pc.vars[e.name]
			if !ok {
				return "", false
			}
			v = val
		case expSubst:
			val, ok := pc.evalSubst(e.inner, depth+1)
			if !ok {
				return "", false
			}
			v = val
		default:
			return "", false
		}
		// Unquoted, the shell would split the value and expand globs in it.
		if !e.quoted && strings.ContainsAny(v, " \t\n*?[") {
			return "", false
		}
		b.WriteString(word[last:e.start])
		b.WriteString(v)
		last = e.end
	}
	b.WriteString(word[last:])
	return b.String(), true
}

// evalWords returns the values of a command's words, or false when one
// depends on something outside the command line.
func (pc pubContext) evalWords(c command, depth int) ([]string, bool) {
	out := make([]string, len(c.words))
	for i, w := range c.words {
		if !c.wordDyn[i] {
			out[i] = w
			continue
		}
		v, ok := pc.evalWord(w, c.wordExps[i], depth)
		if !ok {
			return nil, false
		}
		out[i] = v
	}
	return out, true
}

// evalSubst returns the output of a command substitution whose content is
// in the command line, or in a file the line does not change: cat of a
// here-document or of a file, echo, or printf with the format %s.
func (pc pubContext) evalSubst(inner string, depth int) (string, bool) {
	if depth > maxNesting {
		return "", false
	}
	line := parseShell(inner)
	if line.grouped || len(line.cmds) != 1 {
		return "", false
	}
	c := newCommand(line.cmds[0])
	if len(c.assigns) > 0 || len(c.writes) > 0 || c.pipeFrom >= 0 || len(c.words) == 0 {
		return "", false
	}
	words, ok := pc.evalWords(c, depth)
	if !ok {
		return "", false
	}
	out := ""
	switch c.name() {
	case "cat":
		switch {
		case len(words) == 1 && c.hasStdin && !c.stdinDynamic && c.stdinSources == 1:
			out = c.stdin
		case len(words) == 2 && c.stdinSources == 0 && !strings.HasPrefix(words[1], "-"):
			text, err := pc.readFile(words[1])
			if err != nil {
				return "", false
			}
			out = text
		default:
			return "", false
		}
	case "echo":
		if !plainEcho(words[1:]) {
			return "", false
		}
		out = strings.Join(words[1:], " ")
	case "printf":
		switch {
		case len(words) >= 2 && words[1] == "%s":
			out = strings.Join(words[2:], "")
		case len(words) == 2 && !strings.ContainsAny(words[1], `%\`):
			out = words[1]
		default:
			return "", false
		}
	default:
		return "", false
	}
	return strings.TrimRight(out, "\n"), true
}

// Words that several command and tool names share.
const (
	wordAPI     = "api"
	wordFiles   = "files"
	wordIssue   = "issue"
	wordPush    = "push"
	wordRelease = "release"
	wordTag     = "tag"
)
