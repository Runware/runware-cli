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
	wordCreate  = "create"
	wordGit     = "git"
	wordRepo    = "repo"
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
	case wordGit:
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

// scriptInterpreters run code in another language. The hook searches the
// code given inline (-e, -c, --eval or stdin) for a mention of a gh write or
// git push on one line; hiddenPublication searches all their arguments and
// stdin for one across lines. awk runs a command only through system() or a
// pipe: see awkRunsCode.
var scriptInterpreters = wordSet("bun deno node osascript perl php python python3 ruby")

// inlineCodeFlags are the interpreter flags whose value is code to run.
var inlineCodeFlags = wordSet("-c -e -E -r --command --eval --exec --run")

// execWrappers run their arguments, or some of them, as a command: a gh
// write or git push among their arguments runs, or may: a gh or git word
// there that the check cannot classify is blocked too. In the arguments of
// other commands only a definite publication is blocked.
var execWrappers = wordSet("caffeinate chrt command doas env exec flock ionice nice nohup parallel setsid stdbuf sudo taskset time timeout unbuffer xargs")

// codeRunners run code or a command given in their arguments or on stdin,
// besides the shells, interpreters, evaluators and exec wrappers.
var codeRunners = wordSet("bunx chroot direnv docker expect kubectl mise npx op pipx podman poetry runuser script ssh su tmux unshare uv uvx")

// textTools only read, print or edit text and run nothing they are given,
// unless a script or an option makes them: sed with the e command, awk with
// system(), find -exec, xargs and the command it runs. See runsCode.
var textTools = wordSet("awk column cut find fmt fold gawk mawk nawk nl paste rev sed sort tee tr uniq xargs")

// execFlags make the command they belong to run the words that follow:
// find -exec, for example.
var execFlags = wordSet("-exec -execdir -ok -okdir")

// publicationMention matches text that may run a gh write or git push:
// a gh command with a group that publishes, or git with push later on the
// same line.
var publicationMention = regexp.MustCompile(`\bgh\b[^\n]{0,60}?\b(?:api|gist|issue|label|pr|release|repo)\b|\bgit\b[^\n;|&]{0,80}?\bpush\b`)

// codeToken matches a word of source code in any language: a string, a name
// or a path, without the quotes, commas, brackets and line breaks around it.
var codeToken = regexp.MustCompile(`[A-Za-z0-9_./=@$%+-]+`)

// codePublishes classifies text, source code in any language, as publishes
// classifies a command line: the words after each gh or git, each possibly
// quoted and on its own line as in ["gh", "pr", "comment"], are its
// arguments, and a word with a $ is a variable. A gh followed by a word that
// is no gh group is text ("gh is installed"), unless a group follows within
// a few words, as in Command('gh', {args: ['pr', ...]}).
func codePublishes(text string) pubKind {
	toks := codeToken.FindAllString(text, -1)
	for i, t := range toks {
		base := filepath.Base(t)
		if base != "gh" && base != wordGit {
			continue
		}
		if k := tokensPublish(append([]string{base}, toks[i+1:]...)); k != pubNone {
			return k
		}
	}
	return pubNone
}

// tokensPublish runs publishes on words, a gh or git command found in code.
func tokensPublish(words []string) pubKind {
	dyn := make([]bool, len(words))
	for i := 1; i < len(words); i++ {
		dyn[i] = strings.Contains(words[i], "$")
	}
	kind := publishes(words, dyn)
	if kind != pubUnknown || words[0] != "gh" {
		return kind
	}
	group, _, dynamic := ghCommand(words, dyn)
	if _, known := ghReads[group]; known || dynamic {
		return kind
	}
	for j := 1; j < len(words) && j <= 4; j++ {
		if _, known := ghReads[words[j]]; known || words[j] == wordAPI {
			return tokensPublish(append([]string{"gh"}, words[j:]...))
		}
	}
	return pubNone
}

// definitePublication reports whether text, source code in any language,
// runs or may run a gh write or git push, by the rules of codePublishes.
func definitePublication(text string) bool {
	return codePublishes(text) != pubNone
}

// mentionsPublication reports whether text that runs as code may run a gh
// write or git push: it mentions one on a line, or holds a definite one
// across lines.
func mentionsPublication(text string) bool {
	return publicationMention.MatchString(text) || definitePublication(text)
}

// containsPublication reports whether a command line run in some other way
// (a substitution, a `bash -c` string) publishes anywhere.
func containsPublication(src string, depth int) bool {
	if depth > maxNesting {
		return true
	}
	line := parseShell(src)
	if line.unparsed && mentionsPublication(src) {
		return true
	}
	cmds := commands(line)
	for i := range cmds {
		if publishes(cmds[i].words, cmds[i].wordDyn) != pubNone || hiddenPublication(cmds[i], cmds, depth) != "" {
			return true
		}
	}
	return false
}

// hiddenPublication returns where c runs a publication in a way the hook
// cannot follow, or "": in a substitution, through a wrapper such as xargs or
// any other command that runs its arguments, or in a string or script that a
// shell or interpreter runs.
func hiddenPublication(c command, line []command, depth int) string {
	if depth > maxNesting {
		return "nested too deeply to read"
	}
	for _, src := range c.nested {
		if containsPublication(src, depth+1) {
			return "inside a command substitution"
		}
	}
	if slices.ContainsFunc(c.opaque, mentionsPublication) {
		return "inside a here-document that runs commands"
	}
	if len(c.words) == 0 {
		return ""
	}
	// A command may run a variable set for it: GIT_PAGER, LESSOPEN.
	for _, a := range c.assigns {
		if _, v, _ := strings.Cut(a, "="); containsPublication(v, depth+1) || definitePublication(v) {
			return fmt.Sprintf("in a variable that `%s` may run", c.words[0])
		}
	}
	// What find -exec or xargs runs is read as a command of its own.
	subs := executed(c)
	for i := range subs {
		if where := hiddenPublication(subs[i], line, depth+1); where != "" {
			return where
		}
	}
	if where := xargsInput(c, line); where != "" {
		return where
	}
	// A text tool (echo, grep, sed) only prints, searches or edits its
	// arguments and stdin: a gh write or git push there is text. A command
	// that runs code (uv run, ssh, perl -e, sed with the e command) may run
	// them, and so may a text tool or git given an option that runs a
	// command (rg --pre, git -c). Any other command is blocked only on a gh
	// write or git push that starts one of its words.
	runsOption := runsCodeOption(c)
	codeRuns := runsCode(c)
	_, inertName := inertCommands[c.name()]
	_, textName := textTools[c.name()]
	printsArgs := (inertName || textName) && !c.wordDyn[0] && !runsOption && !codeRuns
	pipedArgs, pipedStdin := pipedInput(c, line)
	runsArgs := false
	for k, w := range c.words {
		base := filepath.Base(w)
		rest := strings.Join(c.words[k+1:], " ")
		if k > 0 {
			kind := publishes(c.words[k:], c.wordDyn[k:])
			if (runsArgs || codeRuns) && kind != pubNone || !printsArgs && (kind == pubGH || kind == pubPush) {
				return fmt.Sprintf("through `%s`", c.words[0])
			}
		}
		if _, ok := execWrappers[base]; ok || c.wordDyn[k] {
			runsArgs = true
		}
		if _, ok := shellKeywords[w]; ok {
			runsArgs = true // do gh ..., if git push ...
		}
		if _, ok := execFlags[w]; ok {
			runsArgs = true
		}
		if printsArgs {
			continue
		}
		if _, ok := shells[base]; ok {
			piped := mentionsPublication(pipedArgs+pipedStdin) || containsPublication(pipedStdin, depth+1)
			script := slices.ContainsFunc(c.words[k+1:], func(w string) bool {
				return containsPublication(w, depth+1)
			})
			if script || containsPublication(rest, depth+1) || containsPublication(c.stdin, depth+1) || piped {
				return fmt.Sprintf("in a script that `%s` runs", base)
			}
		}
		if _, ok := evaluators[base]; ok && containsPublication(rest, depth+1) {
			return fmt.Sprintf("inside `%s`", base)
		}
		if _, ok := scriptInterpreters[base]; ok && publicationMention.MatchString(inlineCode(c.words[k+1:], c.stdin+pipedStdin)) {
			return fmt.Sprintf("inside a `%s` script", base)
		}
	}
	if !codeRuns && !runsOption {
		return ""
	}
	// A command that runs code is blocked on a gh or git command that
	// codePublishes does not clear, anywhere in its arguments (or an
	// option's value after =) or its stdin. A text tool or git that runs a
	// command from its script or an option (sed e, awk system(), rg --pre,
	// git -c) is blocked on any mention of one.
	var code strings.Builder
	for _, w := range c.words[1:] {
		code.WriteString(w + "\n")
		if _, v, ok := strings.Cut(w, "="); ok {
			code.WriteString(v + "\n")
		}
	}
	code.WriteString(c.stdin + "\n" + pipedStdin)
	if stdin, ok := literalStdin(c, line); ok {
		code.WriteString("\n" + stdin)
	}
	_, hasCodeOption := codeOptions[c.name()]
	loose := hasCodeOption && runsOption || textName && codeRuns
	if codePublishes(code.String()) != pubNone || loose && mentionsPublication(code.String()) {
		return fmt.Sprintf("in what `%s` runs", c.words[0])
	}
	return ""
}

// runsCode reports whether c runs code or a command given in its arguments
// or on stdin: a shell, an interpreter, an evaluator, an exec wrapper, a
// code runner (ssh, uv run), a command named by a variable, awk with
// system() or a pipe, or sed with the e command. find and xargs run code
// only through the command they run, which executed returns.
func runsCode(c command) bool {
	if c.wordDyn[0] {
		return true
	}
	name := c.name()
	switch name {
	case "xargs", "find":
		return false
	case "awk", "gawk", "mawk", "nawk":
		return awkRunsCode(c.words[1:])
	case "sed", "gsed":
		return sedRunsCode(c.words[1:])
	}
	for _, set := range []map[string]struct{}{shells, scriptInterpreters, evaluators, stdinRunners, execWrappers, codeRunners} {
		if _, ok := set[name]; ok {
			return true
		}
	}
	return false
}

// awkCommand matches awk code that runs a command: system(), or a pipe to or
// from one.
var awkCommand = regexp.MustCompile(`\bsystem\s*\(|\|\s*&?\s*getline|\bprintf?\b[^;}\n]*\|`)

// awkRunsCode reports whether awk with args may run a command: its program
// runs one, or comes from a file the hook does not read.
func awkRunsCode(args []string) bool {
	for _, a := range args {
		if a == "-f" || strings.HasPrefix(a, "--file") || awkCommand.MatchString(a) {
			return true
		}
	}
	return false
}

// sedRunsCode reports whether sed with args may run a command: a script has
// the e command or an s command with the e flag, or comes from a file the
// hook does not read. Without -e, the script is the first argument that is
// not an option, after the suffix that a BSD -i takes as its next argument
// (” or one that starts with a dot).
func sedRunsCode(args []string) bool {
	var scripts, rest []string
	explicit := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-i" && i+1 < len(args) && (args[i+1] == "" || strings.HasPrefix(args[i+1], ".")):
			i++
		case a == "--file" || strings.HasPrefix(a, "--file="):
			return true
		case a == "--expression":
			explicit = true
			if i+1 < len(args) {
				i++
				scripts = append(scripts, args[i])
			}
		case strings.HasPrefix(a, "--expression="):
			explicit = true
			scripts = append(scripts, strings.TrimPrefix(a, "--expression="))
		case strings.HasPrefix(a, "--"):
		case strings.HasPrefix(a, "-") && len(a) > 1:
			script, next, file := sedShortOptions(a, args[i+1:])
			if file {
				return true
			}
			if script != nil {
				explicit = true
				scripts = append(scripts, *script)
				i += next
			}
		default:
			rest = append(rest, a)
		}
	}
	if !explicit && len(rest) > 0 {
		scripts = rest[:1]
	}
	return slices.ContainsFunc(scripts, sedScriptRuns)
}

// sedShortOptions reads a group of sed short options, followed by the
// arguments after it: the script of an -e in it and how many of those
// arguments it takes, and whether an -f names a script file.
func sedShortOptions(a string, after []string) (script *string, next int, file bool) {
	for j := 1; j < len(a); j++ {
		switch a[j] {
		case 'f':
			return nil, 0, true
		case 'i':
			return nil, 0, false // -i takes an attached suffix
		case 'e':
			if j+1 < len(a) {
				s := a[j+1:]
				return &s, 0, false
			}
			if len(after) > 0 {
				return &after[0], 1, false
			}
			return nil, 0, false
		}
	}
	return nil, 0, false
}

// sedScriptRuns reports whether a sed script has the e command or an s
// command with the e flag.
func sedScriptRuns(script string) bool {
	i := 0
	// field skips past the end of a field delimited by d: an s or y field,
	// or a /regex/ address. It reports whether the field is closed.
	field := func(d byte) bool {
		for ; i < len(script); i++ {
			switch script[i] {
			case '\\':
				i++
			case d:
				i++
				return true
			}
		}
		return false
	}
	for i < len(script) {
		// The address: line numbers, $, /regex/, ranges, steps and !.
		for i < len(script) {
			if script[i] == '/' {
				i++
				field('/')
				continue
			}
			if !strings.ContainsRune("0123456789$,+~! \t;{}\n", rune(script[i])) {
				break
			}
			i++
		}
		if i >= len(script) {
			return false
		}
		cmd := script[i]
		i++
		switch cmd {
		case 'e':
			return true
		case 's', 'y':
			if i >= len(script) {
				return false
			}
			d := script[i]
			i++
			if !field(d) {
				return false
			}
			if !field(d) {
				return false // the replacement
			}
			for ; i < len(script) && !strings.ContainsRune(";}\n", rune(script[i])); i++ {
				if cmd == 's' && script[i] == 'e' {
					return true
				}
				if script[i] == 'w' {
					break // the rest of the line is a file name
				}
			}
		}
		// Any other command runs to the end of the line or the next ;.
		for i < len(script) && script[i] != ';' && script[i] != '\n' {
			i++
		}
	}
	return false
}

// xargsValueOptions are the xargs options that take the next word as their
// value.
var xargsValueOptions = wordSet("-a -d -E -I -L -n -P -s --arg-file --delimiter --max-args --max-chars --max-procs --process-slot-var")

// executed returns the commands that c runs from its own words: the command
// xargs runs, and that of each -exec, -execdir, -ok or -okdir of find. Each
// keeps c's stdin and place in the line.
func executed(c command) []command {
	sub := func(i, j int) command {
		return command{
			shellCmd: c.shellCmd,
			words:    c.words[i:j],
			wordDyn:  c.wordDyn[i:j],
			wordSubs: c.wordSubs[i:j],
		}
	}
	var out []command
	switch c.name() {
	case "xargs":
		i := 1
		for i < len(c.words) && strings.HasPrefix(c.words[i], "-") {
			if c.words[i] == "--" {
				i++
				break
			}
			if _, ok := xargsValueOptions[c.words[i]]; ok {
				i++
			}
			i++
		}
		if i < len(c.words) {
			out = append(out, sub(i, len(c.words)))
		}
	case "find":
		for i := 1; i < len(c.words); i++ {
			if _, ok := execFlags[c.words[i]]; !ok {
				continue
			}
			end := i + 1
			for end < len(c.words) && c.words[end] != ";" && c.words[end] != "+" {
				end++
			}
			if end > i+1 {
				out = append(out, sub(i+1, end))
			}
			i = end
		}
	}
	for n := range out {
		out[n].nested = nil
		out[n].opaque = nil
	}
	return out
}

// xargsInput returns why xargs may run a publication from the words it reads
// on stdin, or "". xargs adds them to the command it runs, so a gh or git
// that names no command of its own (xargs gh, xargs gh api, xargs git), or a
// command named by a variable, takes it from stdin, which must then be text
// the hook reads and classifies as no publication.
func xargsInput(c command, line []command) string {
	if c.name() != "xargs" || c.wordDyn[0] {
		return ""
	}
	subs := executed(c)
	if len(subs) == 0 {
		return ""
	}
	sub := subs[0]
	if name := sub.name(); !sub.wordDyn[0] && name != "gh" && name != wordGit {
		return ""
	}
	stdin, ok := literalStdin(c, line)
	for _, w := range c.words[1:] {
		if w == "-a" || strings.HasPrefix(w, "--arg-file") {
			ok = false
		}
	}
	if !ok {
		if commandComplete(sub) {
			return ""
		}
		return "through xargs, with arguments from stdin the check cannot read"
	}
	toks := codeToken.FindAllString(stdin, -1)
	words := append(slices.Clone(sub.words), toks...)
	dyn := append(slices.Clone(sub.wordDyn), make([]bool, len(toks))...)
	if publishes(words, dyn) != pubNone {
		return "through xargs, with arguments from stdin"
	}
	return ""
}

// commandComplete reports whether a gh or git command names its command in
// literal words, so that arguments added after it cannot make it publish:
// gh with a group and a verb, other than gh api, or git with a subcommand.
func commandComplete(c command) bool {
	if c.wordDyn[0] {
		return false
	}
	if c.name() == wordGit {
		i := gitSubcommand(c.words)
		return i < len(c.words) && !anyTrue(c.wordDyn[:i+1])
	}
	group, verb, dynamic := ghCommand(c.words, c.wordDyn)
	return !dynamic && group != "" && group != wordAPI && (verb != "" || ghReads[group] == "*")
}

// literalStdin returns what c reads on stdin when the hook can read all of
// it: a here-document or here-string that expands nothing, or the literal
// arguments of an echo or printf, or a cat here-document, piped straight
// into it; "" when it has no stdin at all. ok is false for any other stdin:
// a file, or the output of another command.
func literalStdin(c command, line []command) (text string, ok bool) {
	switch {
	case c.stdinSources > 1:
		return "", false
	case c.stdinSources == 1:
		if c.stdinFile != "" || c.stdinDynamic || c.pipeFrom >= 0 {
			return "", false
		}
		return c.stdin, true
	case c.pipeFrom < 0:
		return "", true
	case c.pipeFrom >= len(line):
		return "", false
	}
	src := line[c.pipeFrom]
	if src.inlineHeredoc() {
		return src.stdin, true
	}
	name := src.name()
	if (name == "echo" || name == "printf") && !anyTrue(src.wordDyn) && len(src.nested) == 0 &&
		len(src.assigns) == 0 && src.pipeFrom < 0 && src.stdinSources == 0 {
		return strings.Join(src.words[1:], " "), true
	}
	return "", false
}

// pipedInput returns what the commands piped into c, up its pipeline, are
// given: their arguments, which echo or printf print into c, and their
// here-documents, here-strings, which cat passes on.
func pipedInput(c command, line []command) (args, stdin string) {
	var a, s strings.Builder
	for j, prev := c.pipeFrom, len(line); j >= 0 && j < prev; prev, j = j, line[j].pipeFrom {
		a.WriteString("\n" + strings.Join(line[j].args, " "))
		s.WriteString("\n" + line[j].stdin)
	}
	return a.String(), s.String()
}

// stdinRunners run the commands they read on stdin, besides the shells and
// script interpreters.
var stdinRunners = wordSet(". source xargs")

// runsStdin reports whether c, or a command it wraps, runs what it reads on
// stdin.
func runsStdin(c command) bool {
	for _, w := range c.words {
		base := filepath.Base(w)
		_, shell := shells[base]
		_, interpreter := scriptInterpreters[base]
		_, runner := stdinRunners[base]
		if shell || interpreter || runner {
			return true
		}
	}
	return false
}

// stdinFiles returns the files of the `< file` redirections that feed c's
// stdin: its own and those of the commands piped into it.
func stdinFiles(c command, line []command) []redirTarget {
	var out []redirTarget
	if c.stdinFile != "" {
		out = append(out, redirTarget{
			path:    c.stdinFile,
			dynamic: c.stdinFileDynamic,
		})
	}
	for j, prev := c.pipeFrom, len(line); j >= 0 && j < prev; prev, j = j, line[j].pipeFrom {
		if line[j].stdinFile != "" {
			out = append(out, redirTarget{
				path:    line[j].stdinFile,
				dynamic: line[j].stdinFileDynamic,
			})
		}
	}
	return out
}

// codeOptions are, for each command that is otherwise inert or that runs no
// argument as code (git), the options that make it run a command given on
// the command line or in configuration set there: rg --pre, git -c
// core.pager=... or alias.x=!..., git --ext-diff. Short options also match
// with the value attached (-ccore.pager=...), long ones with =.
var codeOptions = map[string]string{
	"rg":    "--pre",
	"tar":   "--to-command --checkpoint-action --use-compress-program -I --info-script --new-volume-script -F --rsh-command",
	wordGit: "-c --config-env --exec-path --ext-diff --extcmd --exec --upload-pack --receive-pack -O --open-files-in-pager",
}

// subscriptBuiltins evaluate an array subscript in their arguments as
// arithmetic, which runs any $( ) in it, even quoted: test -v 'a[$(cmd)]'.
// printf does so only with -v.
var subscriptBuiltins = wordSet("[ [[ declare export local printf readonly test")

// runsCodeOption reports whether c is given an option that runs a command,
// or is a builtin that may run one from its arguments.
func runsCodeOption(c command) bool {
	name := c.name()
	if len(c.words) == 0 || c.wordDyn[0] {
		return false
	}
	if _, ok := subscriptBuiltins[name]; ok && (name != "printf" || slices.Contains(c.words[1:], "-v")) {
		return true
	}
	for _, w := range c.words[1:] {
		for o := range strings.FieldsSeq(codeOptions[name]) {
			long := strings.HasPrefix(o, "--")
			if w == o || long && strings.HasPrefix(w, o+"=") || !long && strings.HasPrefix(w, o) {
				return true
			}
		}
	}
	return false
}

// inlineCode returns the code an interpreter is given inline: the values of
// its inline code flags (-e CODE, -eCODE, --eval=CODE) and its stdin.
func inlineCode(args []string, stdin string) string {
	var b strings.Builder
	b.WriteString(stdin)
	for i, a := range args {
		name, value, inline := strings.Cut(a, "=")
		if _, ok := inlineCodeFlags[name]; ok {
			if !inline && i+1 < len(args) {
				value = args[i+1]
			}
			b.WriteString("\n" + value)
			continue
		}
		if len(a) > 2 && a[0] == '-' && a[1] != '-' {
			if _, ok := inlineCodeFlags[a[:2]]; ok {
				b.WriteString("\n" + a[2:])
			}
		}
	}
	return b.String()
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
	case wordGit:
		return gitInert(c.words, c.wordDyn) && !runsCodeOption(c)
	case "gh":
		return ghInert(c.words, c.wordDyn)
	}
	_, ok := inertCommands[c.name()]
	return ok && !runsCodeOption(c)
}

// gitReadSubcommands change no file in the working tree and no ref a push
// publishes. git fetch is not one: a refspec such as main:main moves a local
// branch.
var gitReadSubcommands = wordSet("add blame cat-file describe diff for-each-ref grep log ls-files ls-remote merge-base push rev-list rev-parse show show-ref status")

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
	for _, w := range words[i+1:] {
		if w == "--output" || strings.HasPrefix(w, "--output=") {
			return false // git diff --output=FILE writes a file
		}
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

// ghValueFlags are, for each gh command that writes to GitHub and is
// checked, the flags that take a value, as long/short. A flag named *-file
// takes a file whose content is published. Flags not listed are taken as
// booleans: a value they take is read as a positional argument. A command
// with no flags listed publishes nothing but its command line.
var ghValueFlags = map[string]string{
	"pr create":             "title/t body/b body-file/F base/B head/H assignee/a label/l milestone/m project/p reviewer/r template/T",
	"pr edit":               "title/t body/b body-file/F base/B add-assignee remove-assignee add-label remove-label add-project remove-project milestone add-reviewer remove-reviewer",
	"pr comment":            "body/b body-file/F",
	"pr review":             "body/b body-file/F",
	"pr merge":              "subject/t body/b body-file/F match-head-commit author-email",
	"pr close":              "comment/c",
	"pr reopen":             "comment/c",
	"pr revert":             "title/t body/b body-file/F",
	"pr lock":               "reason/r",
	"pr unlock":             "",
	"pr ready":              "",
	"pr update-branch":      "",
	"issue create":          "title/t body/b body-file/F assignee/a label/l milestone/m project/p template/T",
	"issue edit":            "title/t body/b body-file/F add-assignee remove-assignee add-label remove-label add-project remove-project milestone",
	"issue comment":         "body/b body-file/F",
	"issue close":           "comment/c reason/r",
	"issue reopen":          "comment/c",
	"issue delete":          "",
	"issue develop":         "base/b branch-repo name/n worktree",
	"issue lock":            "reason/r",
	"issue unlock":          "",
	"issue pin":             "",
	"issue unpin":           "",
	"issue transfer":        "",
	"release create":        "title/t notes/n notes-file/F target discussion-category notes-start-tag",
	"release edit":          "title/t notes/n notes-file/F tag target discussion-category",
	"release delete":        "",
	"release delete-asset":  "",
	"repo create":           "description/d homepage/h team/t template/p gitignore/g license/l source/s remote/r",
	"repo edit":             "description/d homepage/h default-branch add-topic remove-topic visibility template",
	"repo fork":             "fork-name org remote-name",
	"repo rename":           "",
	"repo archive":          "",
	"repo unarchive":        "",
	"repo delete":           "",
	"gist create":           "desc/d filename/f",
	"gist edit":             "add/a desc/d filename/f remove/r",
	"gist rename":           "",
	"gist delete":           "",
	"label create":          "description/d color/c",
	"label edit":            "description/d color/c name/n",
	"label delete":          "",
	"workflow run":          "ref/r raw-field/f field/F",
	"workflow enable":       "",
	"workflow disable":      "",
	"run rerun":             "job/j",
	"run cancel":            "",
	"run delete":            "",
	"cache delete":          "",
	"project create":        "owner title",
	"project edit":          "owner title description readme visibility",
	"project close":         "owner",
	"project delete":        "owner",
	"project copy":          "owner source-owner target-owner title",
	"project link":          "owner repo/R team/T",
	"project unlink":        "owner repo/R team/T",
	"project mark-template": "owner",
	"project field-create":  "owner name data-type single-select-options",
	"project field-delete":  "id",
	"project item-add":      "owner url",
	"project item-archive":  "owner id",
	"project item-create":   "owner title body",
	"project item-delete":   "owner id",
	"project item-edit":     "id title body text number date single-select-option-id iteration-id field-id project-id",
}

// Read subcommands that several gh groups share.
const (
	readList     = "list"
	readListView = "list view"
)

// ghReads are, for each gh group, the subcommands that publish nothing to
// GitHub ("*" for every one). Any other subcommand may publish, and is
// blocked unless ghValueFlags models it; so is a group not listed, which may
// be an alias or an extension.
var ghReads = map[string]string{
	"pr":            "checkout checks diff list status view",
	"issue":         "list status view",
	"release":       "download list verify verify-asset view",
	wordRepo:        "clone gitignore license list read-dir read-file set-default view",
	"gist":          "clone list view",
	"label":         readList,
	"workflow":      readListView,
	"run":           "download list view watch",
	"secret":        readList,
	"variable":      "get list",
	"ruleset":       "check list view",
	"project":       "field-list item-list list view",
	"cache":         readList,
	"discussion":    readListView,
	"agent-task":    readListView,
	"skill":         "install list preview search update",
	"accessibility": "*",
	"actions":       "*",
	"alias":         "*",
	"attestation":   "*",
	"auth":          "*",
	"browse":        "*",
	"co":            "*",
	"codespace":     "*",
	"completion":    "*",
	"config":        "*",
	"copilot":       "*",
	"environment":   "*",
	"exit-codes":    "*",
	"extension":     "*",
	"formatting":    "*",
	"gpg-key":       "*",
	"help":          "*",
	"licenses":      "*",
	"mintty":        "*",
	"org":           "*",
	"preview":       "*",
	"reference":     "*",
	"search":        "*",
	"ssh-key":       "*",
	"status":        "*",
	"telemetry":     "*",
	"version":       "*",
}

// ghNestedReads are the third-level subcommands that only read, for the gh
// subcommands that have them.
var ghNestedReads = map[string]string{
	"issue artifact":  readListView,
	"repo autolink":   readListView,
	"repo deploy-key": readList,
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

// hasBoolFlag reports whether args, parsed as parseFlags does with the given
// value flags, set one of the boolean flags names (long names) or short (a
// shorthand letter, 0 for none), alone or grouped (-df), to anything but
// false.
func hasBoolFlag(args []string, valueFlags string, names []string, short byte) bool {
	long := map[string]struct{}{}
	shorts := map[byte]struct{}{}
	for f := range strings.FieldsSeq(valueFlags) {
		name, sh, _ := strings.Cut(f, "/")
		long[name] = struct{}{}
		if sh != "" {
			shorts[sh[0]] = struct{}{}
		}
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return false
		case strings.HasPrefix(a, "--"):
			name, value, inline := strings.Cut(a[2:], "=")
			if slices.Contains(names, name) && value != "false" {
				return true
			}
			if _, ok := long[name]; ok && !inline {
				i++
			}
		case strings.HasPrefix(a, "-") && len(a) > 1:
			for j := 1; j < len(a); j++ {
				if short != 0 && a[j] == short {
					return true
				}
				if _, ok := shorts[a[j]]; ok {
					if j == len(a)-1 {
						i++
					}
					break
				}
			}
		}
	}
	return false
}

// fillFlags make gh pr create write the title and body from commit messages.
var fillFlags = []string{
	"fill",
	"fill-first",
	"fill-verbose",
}

// ghReadVerbs are the subcommands that publish nothing and change no file.
var ghReadVerbs = wordSet("checks diff list status view")

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
	}
	key := group + " " + verb
	if _, ok := ghValueFlags[key]; ok {
		if key == "repo create" && hasBoolFlag(words[2:], ghValueFlags[key], []string{wordPush}, 0) {
			return pubUnknown // it pushes local commits, which the check does not scan
		}
		return pubGH
	}
	reads, ok := ghReads[group]
	switch {
	case !ok:
		return pubUnknown
	case reads == "*" || verb == "" || strings.HasPrefix(verb, "-"):
		return pubNone
	case slices.Contains(strings.Fields(reads), verb):
		return pubNone
	}
	if nested, ok := ghNestedReads[key]; ok {
		_, pos := parseFlags(words[1:], "repo/R")
		if len(pos) > 2 && !dyn[1+pos[2]] && slices.Contains(strings.Fields(nested), words[1+pos[2]]) {
			return pubNone
		}
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

// apiReadFlags are the gh api value flags that change only how a response
// is shown or fetched: a variable in their value cannot make a call write.
var apiReadFlags = wordSet("--cache --header -H --hostname --jq -q --preview -p --template -t")

// apiWrites reports whether a gh api call may write: it has fields or an
// input, a method other than GET, or a variable where it could add one. A
// variable inside an endpoint that starts with a literal path, such as
// repos/o/r/pulls/$PR/comments, or in the value of a flag such as --jq, is a
// read; an endpoint that is wholly a variable ($ARGS, "$@") may hold flags
// too, and is a read only with an explicit GET after it.
func apiWrites(words []string, dyn []bool) bool {
	flags, pos := parseFlags(words[1:], apiValueFlags)
	explicitGET := false
	// method is the index of the last word that sets the method: a GET
	// there makes a variable before it a read, but not one after it, which
	// may set the method again.
	method := 0
	for i, w := range words {
		if strings.HasPrefix(w, "-X") || strings.HasPrefix(w, "--method") {
			method = i
		}
	}
	for _, f := range flags {
		switch f.name {
		case "method":
			if !strings.EqualFold(f.value, "GET") {
				return true
			}
			explicitGET = true
		case "raw-field", "field", "input":
			return true
		}
	}
	for i := 1; i < len(words); i++ {
		if !dyn[i] {
			continue
		}
		name, _, inline := strings.Cut(words[i], "=")
		_, readValue := apiReadFlags[name]
		_, afterRead := apiReadFlags[words[i-1]]
		isEndpoint := len(pos) > 1 && i == pos[1]+1 && (literalPath.MatchString(words[i]) || explicitGET && i < method)
		if !isEndpoint && (!inline || !readValue) && (!afterRead || dyn[i-1]) {
			return true
		}
	}
	return false
}

// literalPath matches a gh api endpoint that starts with a literal path
// segment, such as repos/, /orgs or graphql.
var literalPath = regexp.MustCompile(`^/?[A-Za-z][A-Za-z0-9_.-]*(?:/|$)`)

var prURL = regexp.MustCompile(`^https?://(?:www\.)?github\.com/([^/]+/[^/]+)/(?:pull|issues)/`)

// ghURLRepo returns the owner/name of a pull request or issue URL, or "".
func ghURLRepo(s string) string {
	m := prURL.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return m[1]
}
