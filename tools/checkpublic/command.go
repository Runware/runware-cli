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
// that nothing in the same line changes, and stdin from one source the hook
// can read. Anything else is blocked with UncheckableError.
//
// The hook evaluates nothing. When a line holds a gh write to a repository
// that is not private, it scans the whole line, so text written in the line
// itself (a literal --body, a quoted here-document, $(cat <<'EOF' ... EOF))
// needs no evaluation; only files the command posts are read.

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

// Words that several command and tool names share.
const (
	wordAPI     = "api"
	wordFiles   = "files"
	wordIssue   = "issue"
	wordPush    = "push"
	wordRelease = "release"
	wordTag     = "tag"
)

// command is a simple command with its leading assignments and wrappers
// removed.
type command struct {
	shellCmd
	words     []string
	wordDyn   []bool
	wordSubs  [][]string
	assigns   []string
	assignDyn []bool
}

var envAssignment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*\+?=`)

// simpleWrappers run their arguments as a command without changing it.
var simpleWrappers = wordSet("command env exec nohup time")

// wordSet returns the set of the space-separated words in s.
func wordSet(s string) map[string]struct{} {
	out := map[string]struct{}{}
	for w := range strings.FieldsSeq(s) {
		out[w] = struct{}{}
	}
	return out
}

func commands(line shellLine) []command {
	out := make([]command, len(line.cmds))
	for n := range line.cmds {
		c := &line.cmds[n]
		i := 0
		for ; i < len(c.args); i++ {
			if envAssignment.MatchString(c.args[i]) {
				out[n].assigns = append(out[n].assigns, c.args[i])
				out[n].assignDyn = append(out[n].assignDyn, c.dyn[i])
				continue
			}
			if _, ok := simpleWrappers[c.args[i]]; !ok || c.dyn[i] {
				break
			}
		}
		out[n].shellCmd = *c
		out[n].words = c.args[i:]
		out[n].wordDyn = c.dyn[i:]
		out[n].wordSubs = c.subs[i:]
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

// inlineHeredoc reports whether c is `cat` reading a here-document or
// here-string that expands nothing: its output is text of the command line.
func (c command) inlineHeredoc() bool {
	return len(c.words) == 1 && c.name() == "cat" && !c.wordDyn[0] && len(c.assigns) == 0 &&
		c.stdinSources == 1 && c.hasStdin && !c.stdinDynamic && c.pipeFrom < 0 && len(c.writes) == 0
}

// inLine reports whether a word's expansions are all $(cat <<'EOF' ... EOF):
// its value is then text of the command line, which the hook scans whole.
func inLine(subs []string) bool {
	for _, s := range subs {
		line := parseShell(s)
		cmds := commands(line)
		if s == "" || line.grouped || len(cmds) != 1 || !cmds[0].inlineHeredoc() {
			return false
		}
	}
	return true
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
var writeWords = wordSet("api close comment create edit gh git issue merge pr push release reopen review")

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
		i := gitSubcommand(words)
		switch {
		case i >= len(words):
		case anyTrue(dyn[1 : i+1]):
			return pubUnknown
		case words[i] == wordPush:
			return pubPush
		}
	}
	return pubNone
}

// gitValueOptions are the git options before the subcommand that take a value.
var gitValueOptions = wordSet("-C -c --exec-path --git-dir --namespace --work-tree")

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

// shells run a command line given as an argument or on stdin.
var shells = wordSet("bash dash fish ksh sh zsh")

// evaluators run their arguments as a command line.
var evaluators = wordSet("alias eval trap watch")

// scriptInterpreters run code in another language, which the hook only
// searches for gh and push.
var scriptInterpreters = wordSet("bun deno node osascript perl php python python3 ruby")

var publicationMention = regexp.MustCompile(`\bgh\b|\bpush\b`)

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
	if slices.ContainsFunc(c.opaque, publicationMention.MatchString) {
		return "inside a here-document that runs commands"
	}
	for k, w := range c.words {
		base := filepath.Base(w)
		rest := strings.Join(c.words[k+1:], " ")
		if k > 0 && publishes(c.words[k:], c.wordDyn[k:]) != pubNone {
			return fmt.Sprintf("through `%s`", c.words[0])
		}
		if _, ok := shells[base]; ok {
			piped := c.pipeFrom >= 0 && c.pipeFrom < len(line) && publicationMention.MatchString(strings.Join(line[c.pipeFrom].args, " "))
			if containsPublication(rest, depth+1) || containsPublication(c.stdin, depth+1) || piped {
				return fmt.Sprintf("in a script that `%s` runs", base)
			}
		}
		if _, ok := evaluators[base]; ok && containsPublication(rest, depth+1) {
			return fmt.Sprintf("inside `%s`", base)
		}
		if _, ok := scriptInterpreters[base]; ok && publicationMention.MatchString(rest+"\n"+c.stdin) {
			return fmt.Sprintf("inside a `%s` script", base)
		}
	}
	return ""
}

// shellKeywords start or end a compound command, whose body the shell runs
// later, repeatedly or not at all.
var shellKeywords = wordSet("! case do done elif else esac fi for function if select then until while { }")

// alongside returns the indexes of the commands of a line that run before
// cmds[i] or at the same time: every earlier command, the rest of its
// pipeline, and every later command when its pipeline runs in the background.
func alongside(cmds []command, i int) []int {
	end := i
	for end < len(cmds)-1 && (cmds[end].sep == "|" || cmds[end].sep == "|&") {
		end++
	}
	if cmds[end].sep == "&" {
		end = len(cmds) - 1
	}
	var out []int
	for j := 0; j <= end; j++ {
		if j != i {
			out = append(out, j)
		}
	}
	return out
}

// sensitiveEnv are the variables that change where gh or git publishes.
var sensitiveEnv = wordSet("GH_HOST GH_REPO GIT_DIR GIT_NAMESPACE GIT_WORK_TREE")

func setsSensitive(assigns []string) bool {
	for _, a := range assigns {
		name, _, _ := strings.Cut(strings.Replace(a, "+=", "=", 1), "=")
		if _, ok := sensitiveEnv[name]; ok {
			return true
		}
	}
	return false
}

// inertCommands neither write files nor change git refs. cd is followed by
// the caller; a declaration that sets a sensitive variable is not inert.
var inertCommands = wordSet(": [ [[ cat cd date diff echo false grep head jq ls printf pwd rg sleep tail test true wc which declare export local readonly")

// inert reports whether c can run before a publication without changing
// what it publishes: it, and every substitution in it, writes no file (apart
// from its own redirections, which are checked on their own) and changes no
// git ref or gh destination.
func inert(c command) bool {
	if setsSensitive(c.assigns) || setsSensitive(c.words) {
		return false
	}
	for _, src := range c.nested {
		nested := commands(parseShell(src))
		for i := range nested {
			if len(nested[i].writes) > 0 || !inert(nested[i]) {
				return false
			}
		}
	}
	if len(c.words) == 0 {
		return true
	}
	if c.wordDyn[0] {
		return false
	}
	switch c.name() {
	case "git":
		return gitInert(c.words, c.wordDyn)
	case "gh":
		return ghInert(c.words, c.wordDyn)
	}
	_, ok := inertCommands[c.name()]
	return ok
}

// gitReadSubcommands change no file in the working tree and no ref a push
// publishes.
var gitReadSubcommands = wordSet("add blame cat-file describe diff fetch for-each-ref grep log ls-files ls-remote merge-base push rev-list rev-parse show show-ref status")

func gitInert(words []string, dyn []bool) bool {
	i := 1
	for i < len(words) && (words[i] == "-C" || words[i] == "--no-pager") {
		if words[i] == "-C" {
			i++
		}
		i++
	}
	if i >= len(words) {
		return i == len(words) && !anyTrue(dyn)
	}
	if anyTrue(dyn[:i+1]) {
		return false
	}
	rest := strings.Join(words[i+1:], " ")
	switch words[i] {
	case "branch":
		return rest == "" || rest == "--show-current"
	case "remote":
		return rest == "" || rest == "-v" || strings.HasPrefix(rest, "get-url")
	}
	_, ok := gitReadSubcommands[words[i]]
	return ok
}

// ghValueFlags are, for each gh command that publishes a title, body or
// comment, the flags that take a value, as long/short. A flag named *-file
// takes a file whose content is published. Flags not listed are taken as
// booleans: a value they take is read as a positional argument.
var ghValueFlags = map[string]string{
	"pr create":      "title/t body/b body-file/F base/B head/H",
	"pr edit":        "title/t body/b body-file/F base/B add-assignee remove-assignee add-label remove-label add-project remove-project milestone add-reviewer remove-reviewer",
	"pr comment":     "body/b body-file/F",
	"pr review":      "body/b body-file/F",
	"pr merge":       "subject/t body/b body-file/F match-head-commit author-email",
	"pr close":       "comment/c",
	"pr reopen":      "comment/c",
	"issue create":   "title/t body/b body-file/F",
	"issue edit":     "title/t body/b body-file/F add-assignee remove-assignee add-label remove-label add-project remove-project milestone",
	"issue comment":  "body/b body-file/F",
	"issue close":    "comment/c reason/r",
	"issue reopen":   "comment/c",
	"release create": "title/t notes/n notes-file/F target discussion-category notes-start-tag",
	"release edit":   "title/t notes/n notes-file/F tag target discussion-category",
}

// apiValueFlags are the gh api flags that take a value.
const apiValueFlags = "method/X raw-field/f field/F input header/H jq/q template/t hostname preview/p cache"

// flagVal is one flag found on a command line.
type flagVal struct {
	name  string
	value string
}

// parseFlags splits args as pflag does, given the flags that take a value
// (see ghValueFlags): --name value, --name=value, -x value, -xvalue, -x=value
// and boolean shorthands grouped before a value flag (-wbtext), with --
// ending the flags. It returns the flags and the indexes of the positional
// arguments.
func parseFlags(args []string, valueFlags string) ([]flagVal, []int) {
	long := map[string]struct{}{}
	short := map[byte]string{}
	for f := range strings.FieldsSeq(valueFlags) {
		name, s, _ := strings.Cut(f, "/")
		long[name] = struct{}{}
		if s != "" {
			short[s[0]] = name
		}
	}
	var flags []flagVal
	var pos []int
	for i := 0; i < len(args); i++ {
		a := args[i]
		var f flagVal
		inline := false
		switch {
		case a == "--":
			for j := i + 1; j < len(args); j++ {
				pos = append(pos, j)
			}
			return flags, pos
		case strings.HasPrefix(a, "--"):
			f.name, f.value, inline = strings.Cut(a[2:], "=")
			if _, ok := long[f.name]; !ok {
				continue
			}
		case strings.HasPrefix(a, "-") && len(a) > 1:
			j := 1
			for j < len(a) && short[a[j]] == "" {
				j++
			}
			if j == len(a) {
				continue
			}
			f.name = short[a[j]]
			f.value = strings.TrimPrefix(a[j+1:], "=")
			inline = j+1 < len(a)
		default:
			pos = append(pos, i)
			continue
		}
		if !inline && i+1 < len(args) {
			i++
			f.value = args[i]
		}
		flags = append(flags, f)
	}
	return flags, pos
}

// ghReadVerbs are the subcommands that publish nothing and change no file.
var ghReadVerbs = wordSet("checks diff list status view")

// ghOtherGroups are the gh commands that publish no title, body or comment
// this hook checks. Anything else may be an alias or an extension.
var ghOtherGroups = wordSet("alias attestation auth browse cache co codespace completion config extension gist gpg-key help label org project repo ruleset run search secret ssh-key status variable version workflow")

// ghCommand returns the group and verb of a gh command line ("" when it has
// none), and whether either is dynamic.
func ghCommand(words []string, dyn []bool) (group, verb string, dynamic bool) {
	_, pos := parseFlags(words[1:], "repo/R")
	if len(pos) > 0 {
		group, dynamic = words[1+pos[0]], dyn[1+pos[0]]
	}
	if len(pos) > 1 {
		verb, dynamic = words[1+pos[1]], dynamic || dyn[1+pos[1]]
	}
	return group, verb, dynamic
}

func ghPublishes(words []string, dyn []bool) pubKind {
	group, verb, dynamic := ghCommand(words, dyn)
	switch {
	case group == "":
		return pubNone
	case group == wordAPI:
		if apiWrites(words, dyn) {
			return pubGH
		}
		return pubNone
	case dynamic:
		return pubUnknown
	case group == "pr" || group == wordIssue || group == wordRelease:
		if _, ok := ghValueFlags[group+" "+verb]; ok {
			return pubGH
		}
		return pubNone
	}
	if _, ok := ghOtherGroups[group]; ok {
		return pubNone
	}
	return pubUnknown
}

// ghInert reports whether a gh command changes nothing a later publication
// reads: a read, or a publication itself.
func ghInert(words []string, dyn []bool) bool {
	if anyTrue(dyn) {
		return false
	}
	switch publishes(words, dyn) {
	case pubGH:
		return true
	case pubUnknown:
		return false
	}
	group, verb, _ := ghCommand(words, dyn)
	_, ok := ghReadVerbs[verb]
	return ok || verb == "" || group == wordAPI
}

// apiWrites reports whether a gh api call may write: it has fields or an
// input, a method other than GET, or arguments the hook cannot read.
func apiWrites(words []string, dyn []bool) bool {
	if anyTrue(dyn) {
		return true
	}
	flags, _ := parseFlags(words[1:], apiValueFlags)
	for _, f := range flags {
		switch f.name {
		case "method":
			if !strings.EqualFold(f.value, "GET") {
				return true
			}
		case "raw-field", "field", "input":
			return true
		}
	}
	return false
}

var prURL = regexp.MustCompile(`^https?://(?:www\.)?github\.com/([^/]+/[^/]+)/(?:pull|issues)/`)

// ghURLRepo returns the owner/name of a pull request or issue URL, or "".
func ghURLRepo(s string) string {
	m := prURL.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return m[1]
}
