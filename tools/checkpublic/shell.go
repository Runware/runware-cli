package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// redirTarget is the file of an output redirection.
type redirTarget struct {
	path string
	// dynamic is set when the name holds an expansion the lexer does not
	// evaluate, so it could be any file.
	dynamic bool
}

// expKind is the kind of an expansion in a word.
type expKind int

const (
	// expOther is an expansion the hook does not evaluate: a glob, a brace
	// or tilde expansion, a special parameter, a process substitution.
	expOther expKind = iota
	// expVar is $NAME or ${NAME}.
	expVar
	// expSubst is $(...) or a backtick substitution.
	expSubst
)

// expansion is one expansion in a word, at word[start:end] in its source form.
type expansion struct {
	start  int
	end    int
	kind   expKind
	name   string
	inner  string
	quoted bool
}

// shellCmd is one simple command of a shell command line, after quote
// removal, with what it reads on stdin when the command line says so.
type shellCmd struct {
	args []string
	// dyn[i] is set when args[i] holds a variable, command substitution or
	// glob that the lexer does not evaluate: the shell may pass anything.
	dyn []bool
	// exps[i] are the expansions in args[i].
	exps [][]expansion
	// stdin is the content of a here-document or here-string, or of a
	// `cat <<EOF` piped into the command.
	stdin    string
	hasStdin bool
	// stdinDynamic is set when that here-document or here-string expands
	// variables or commands.
	stdinDynamic bool
	// stdinFile is the file of a `< file` redirection, or of a `cat file`
	// piped into the command.
	stdinFile        string
	stdinFileDynamic bool
	// stdinSources counts the redirections that feed stdin: <, << and <<<.
	stdinSources int
	// pipeFrom is the index of the command piped into this one, or -1.
	pipeFrom int
	// writes are the targets of the command's output redirections.
	writes []redirTarget
	// nested are the sources of the command and process substitutions in
	// the command's words, which the shell runs before the command.
	nested []string
	// opaque are here-document bodies that expand commands: the shell runs
	// them, but they are text, not a command line the lexer can split.
	opaque []string
	// sep is the operator after the command: ";", "&&", "||", "&", "|",
	// "|&", "\n", or "" at the end of the line.
	sep string
}

// shellLine is a parsed command line.
type shellLine struct {
	cmds []shellCmd
	// grouped is set when the line has parentheses outside a substitution:
	// a subshell, a function definition or a case pattern.
	grouped bool
}

type pendingHeredoc struct {
	delim  string
	strip  bool
	quoted bool
	cmd    int
}

// lexer splits a command line into simple commands. It understands quoting,
// escapes, command separators, pipes, redirections, here-documents and
// command substitution well enough to recover the arguments a hook has to
// check. It evaluates nothing: a word with $VAR, $(...), a backtick or a
// glob keeps its source text and is marked dynamic.
type lexer struct {
	src  string
	i    int
	line shellLine

	cur     shellCmd
	word    strings.Builder
	inWord  bool
	wordDyn bool
	// wordExps are the expansions in the current word.
	wordExps []expansion
	// wordBrace is set when the word has an unquoted {, which with a comma
	// or .. is a brace expansion.
	wordBrace bool
	// wordTilde is set when the word starts with an unquoted ~.
	wordTilde bool

	// Where the next word goes instead of the argument list.
	writeNext      bool
	stdinFileNext  bool
	hereStringNext bool

	heredocs []pendingHeredoc
}

// parseShell parses src.
func parseShell(src string) shellLine {
	lx := &lexer{
		src: src,
	}
	lx.cur.pipeFrom = -1
	lx.run()
	lx.resolvePipes()
	return lx.line
}

func (lx *lexer) run() {
	for lx.i < len(lx.src) {
		c := lx.src[lx.i]
		switch {
		case c == ' ' || c == '\t':
			lx.endWord()
			lx.i++
		case c == '\n':
			lx.endWord()
			lx.endCmd("\n")
			lx.i++
			lx.readHeredocs()
		case c == ';':
			lx.endWord()
			lx.endCmd(";")
			lx.i++
		case c == '(' || c == ')':
			lx.endWord()
			lx.endCmd(string(c))
			lx.line.grouped = true
			lx.i++
		case c == '&':
			if lx.peek(1) == '>' {
				lx.redirectOut()
				continue
			}
			lx.endWord()
			lx.i++
			if lx.peek(0) == '&' {
				lx.i++
				lx.endCmd("&&")
				continue
			}
			lx.endCmd("&")
		case c == '|':
			lx.endWord()
			lx.i++
			switch lx.peek(0) {
			case '|':
				lx.i++
				lx.endCmd("||")
				continue
			case '&':
				lx.i++
				lx.endCmd("|&")
			default:
				lx.endCmd("|")
			}
			if len(lx.line.cmds) > 0 {
				lx.cur.pipeFrom = len(lx.line.cmds) - 1
			}
		case c == '<' && lx.peek(1) == '(', c == '>' && lx.peek(1) == '(':
			lx.startWord()
			src, _ := lx.substitution()
			lx.addExp(expansion{
				kind: expOther,
			}, src)
		case c == '<':
			lx.redirectIn()
		case c == '>':
			lx.redirectOut()
		case c == '#' && !lx.inWord:
			for lx.i < len(lx.src) && lx.src[lx.i] != '\n' {
				lx.i++
			}
		case c == '\\':
			if lx.peek(1) == '\n' {
				lx.i += 2
				continue
			}
			lx.startWord()
			if lx.i+1 < len(lx.src) {
				lx.word.WriteByte(lx.src[lx.i+1])
			}
			lx.i += 2
		case c == '\'':
			lx.startWord()
			end := strings.IndexByte(lx.src[lx.i+1:], '\'')
			if end < 0 {
				lx.word.WriteString(lx.src[lx.i+1:])
				lx.i = len(lx.src)
				continue
			}
			lx.word.WriteString(lx.src[lx.i+1 : lx.i+1+end])
			lx.i += end + 2
		case c == '"':
			lx.startWord()
			lx.i++
			lx.doubleQuoted()
		case c == '$' && lx.peek(1) == '\'':
			lx.startWord()
			lx.i += 2
			lx.ansiQuoted()
		case c == '$' && lx.expands():
			lx.startWord()
			lx.dollar(false)
		case c == '`':
			lx.startWord()
			lx.backtick(false)
		default:
			if c == '~' && !lx.inWord {
				lx.wordTilde = true
			}
			lx.startWord()
			switch c {
			case '*', '?', '[':
				lx.addExp(expansion{
					kind: expOther,
				}, string(c))
			case '{':
				lx.wordBrace = true
				lx.word.WriteByte(c)
			default:
				lx.word.WriteByte(c)
			}
			lx.i++
		}
	}
	lx.endWord()
	lx.endCmd("")
}

func (lx *lexer) peek(n int) byte {
	if lx.i+n < len(lx.src) {
		return lx.src[lx.i+n]
	}
	return 0
}

func (lx *lexer) startWord() {
	lx.inWord = true
}

// expands reports whether the $ at lx.i starts an expansion.
func (lx *lexer) expands() bool {
	n := lx.peek(1)
	return n == '(' || n == '{' || n == '_' || n >= 'a' && n <= 'z' || n >= 'A' && n <= 'Z' || n >= '0' && n <= '9' || strings.IndexByte("@*#?$!-", n) >= 0
}

// addExp appends src, the source text of an expansion, to the word and
// records the expansion.
func (lx *lexer) addExp(e expansion, src string) {
	lx.wordDyn = true
	e.start = lx.word.Len()
	lx.word.WriteString(src)
	e.end = lx.word.Len()
	lx.wordExps = append(lx.wordExps, e)
}

var shellName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// dollar reads an expansion at lx.i and keeps its source text.
func (lx *lexer) dollar(quoted bool) {
	if lx.peek(1) == '(' {
		src, inner := lx.substitution()
		kind := expSubst
		if strings.HasPrefix(inner, "(") {
			kind = expOther // $(( arithmetic ))
		}
		lx.addExp(expansion{
			kind:   kind,
			inner:  inner,
			quoted: quoted,
		}, src)
		return
	}
	start := lx.i
	lx.i++
	name := ""
	if lx.peek(0) == '{' {
		end := strings.IndexByte(lx.src[lx.i:], '}')
		if end < 0 {
			lx.i = len(lx.src)
		} else {
			name = lx.src[lx.i+1 : lx.i+end]
			lx.i += end + 1
		}
	} else {
		lx.i++
		for lx.i < len(lx.src) && isNameByte(lx.src[lx.i]) && !isDigit(lx.src[start+1]) {
			lx.i++
		}
		name = lx.src[start+1 : lx.i]
	}
	e := expansion{
		kind:   expOther,
		quoted: quoted,
	}
	if shellName.MatchString(name) {
		e.kind = expVar
		e.name = name
	}
	lx.addExp(e, lx.src[start:lx.i])
}

func isNameByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func (lx *lexer) endWord() {
	if !lx.inWord {
		return
	}
	w := lx.word.String()
	dyn := lx.wordDyn
	exps := lx.wordExps
	if lx.wordBrace && (strings.Contains(w, ",") || strings.Contains(w, "..")) ||
		lx.wordTilde && w != "~" && !strings.HasPrefix(w, "~/") {
		dyn = true
		exps = append(exps, expansion{
			kind: expOther,
		})
	}
	lx.word.Reset()
	lx.inWord = false
	lx.wordDyn = false
	lx.wordExps = nil
	lx.wordBrace = false
	lx.wordTilde = false
	switch {
	case lx.writeNext:
		lx.writeNext = false
		lx.cur.writes = append(lx.cur.writes, redirTarget{
			path:    w,
			dynamic: dyn,
		})
	case lx.stdinFileNext:
		lx.stdinFileNext = false
		lx.cur.stdinFile = w
		lx.cur.stdinFileDynamic = dyn
	case lx.hereStringNext:
		lx.hereStringNext = false
		lx.cur.stdin = w + "\n"
		lx.cur.hasStdin = true
		lx.cur.stdinDynamic = dyn
	default:
		lx.cur.args = append(lx.cur.args, w)
		lx.cur.dyn = append(lx.cur.dyn, dyn)
		lx.cur.exps = append(lx.cur.exps, exps)
	}
}

func (lx *lexer) endCmd(sep string) {
	c := lx.cur
	if len(c.args) > 0 || c.stdinSources > 0 || len(c.writes) > 0 || len(c.nested) > 0 {
		c.sep = sep
		lx.line.cmds = append(lx.line.cmds, c)
	}
	lx.cur = shellCmd{
		pipeFrom: -1,
	}
}

// dropFD drops a word that is only a file descriptor number before a redirection.
func (lx *lexer) dropFD() {
	if lx.inWord && strings.Trim(lx.word.String(), "0123456789") == "" && !lx.wordDyn {
		lx.word.Reset()
		lx.inWord = false
	}
	lx.endWord()
}

func (lx *lexer) redirectOut() {
	lx.dropFD()
	for lx.i < len(lx.src) && strings.IndexByte(">&|", lx.src[lx.i]) >= 0 {
		lx.i++
	}
	// >&2 and 2>&1 name a descriptor, not a file.
	if lx.src[lx.i-1] == '&' && lx.i < len(lx.src) && (lx.src[lx.i] >= '0' && lx.src[lx.i] <= '9' || lx.src[lx.i] == '-') {
		for lx.i < len(lx.src) && (lx.src[lx.i] >= '0' && lx.src[lx.i] <= '9' || lx.src[lx.i] == '-') {
			lx.i++
		}
		return
	}
	lx.writeNext = true
}

func (lx *lexer) redirectIn() {
	lx.dropFD()
	switch {
	case strings.HasPrefix(lx.src[lx.i:], "<<<"):
		lx.i += 3
		lx.cur.stdinSources++
		lx.hereStringNext = true
	case strings.HasPrefix(lx.src[lx.i:], "<<"):
		lx.i += 2
		strip := false
		if lx.peek(0) == '-' {
			strip = true
			lx.i++
		}
		delim, quoted := lx.readDelimiter()
		lx.cur.stdinSources++
		lx.heredocs = append(lx.heredocs, pendingHeredoc{
			delim:  delim,
			strip:  strip,
			quoted: quoted,
			cmd:    len(lx.line.cmds),
		})
	default:
		lx.i++
		if lx.peek(0) == '&' || lx.peek(0) == '>' {
			lx.i++
		}
		lx.cur.stdinSources++
		lx.stdinFileNext = true
	}
}

// readDelimiter reads a here-document delimiter word, removing its quotes,
// and reports whether it was quoted: the body of a here-document with an
// unquoted delimiter expands variables and commands.
func (lx *lexer) readDelimiter() (string, bool) {
	for lx.i < len(lx.src) && (lx.src[lx.i] == ' ' || lx.src[lx.i] == '\t') {
		lx.i++
	}
	var b strings.Builder
	quoted := false
	for lx.i < len(lx.src) {
		c := lx.src[lx.i]
		if c == '\'' || c == '"' || c == '\\' {
			quoted = true
			lx.i++
			continue
		}
		if strings.IndexByte(" \t\n;&|<>()", c) >= 0 {
			break
		}
		b.WriteByte(c)
		lx.i++
	}
	return b.String(), quoted
}

// readHeredocs reads the bodies of the here-documents started on the line
// that just ended. Each belongs to the command that was open when it started,
// which endCmd has appended by now.
func (lx *lexer) readHeredocs() {
	for _, h := range lx.heredocs {
		body := readHeredocBody(lx.src, &lx.i, h.delim, h.strip)
		if h.cmd >= len(lx.line.cmds) {
			continue
		}
		c := &lx.line.cmds[h.cmd]
		c.stdin = body
		c.hasStdin = true
		if !h.quoted && strings.ContainsAny(body, "$`\\") {
			c.stdinDynamic = true
			if strings.Contains(body, "$(") || strings.Contains(body, "`") {
				c.opaque = append(c.opaque, body)
			}
		}
	}
	lx.heredocs = nil
}

// readHeredocBody reads lines from src at *i up to the delimiter line.
func readHeredocBody(src string, i *int, delim string, strip bool) string {
	var b strings.Builder
	for *i < len(src) {
		end := strings.IndexByte(src[*i:], '\n')
		var line string
		if end < 0 {
			line = src[*i:]
			*i = len(src)
		} else {
			line = src[*i : *i+end]
			*i += end + 1
		}
		check := line
		if strip {
			check = strings.TrimLeft(line, "\t")
		}
		if check == delim {
			break
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

func (lx *lexer) doubleQuoted() {
	for lx.i < len(lx.src) {
		c := lx.src[lx.i]
		switch {
		case c == '"':
			lx.i++
			return
		case c == '\\' && lx.i+1 < len(lx.src):
			next := lx.src[lx.i+1]
			switch next {
			case '"', '\\', '$', '`':
				lx.word.WriteByte(next)
			case '\n':
			default:
				lx.word.WriteByte(c)
				lx.word.WriteByte(next)
			}
			lx.i += 2
		case c == '$' && lx.expands():
			lx.dollar(true)
		case c == '`':
			lx.backtick(true)
		default:
			lx.word.WriteByte(c)
			lx.i++
		}
	}
}

func (lx *lexer) ansiQuoted() {
	for lx.i < len(lx.src) {
		c := lx.src[lx.i]
		switch {
		case c == '\'':
			lx.i++
			return
		case c == '\\' && lx.i+1 < len(lx.src):
			switch next := lx.src[lx.i+1]; next {
			case 'n':
				lx.word.WriteByte('\n')
			case 't':
				lx.word.WriteByte('\t')
			case '\\', '\'', '"', '?':
				lx.word.WriteByte(next)
			default:
				// Octal, hex and other escapes: keep the source and let a
				// publication that holds one be blocked.
				lx.addExp(expansion{
					kind: expOther,
				}, string([]byte{c, next}))
			}
			lx.i += 2
		default:
			lx.word.WriteByte(c)
			lx.i++
		}
	}
}

// substitution reads $( ... ), <( ... ) or >( ... ) at lx.i, records its
// inner text as a nested command line and returns its source and inner text.
func (lx *lexer) substitution() (string, string) {
	start := lx.i
	lx.i += 2
	inner := captureParens(lx.src, &lx.i)
	lx.cur.nested = append(lx.cur.nested, inner)
	return lx.src[start:lx.i], inner
}

// backtick reads a backtick substitution at lx.i into the word.
func (lx *lexer) backtick(quoted bool) {
	start := lx.i
	lx.i++
	end := strings.IndexByte(lx.src[lx.i:], '`')
	inner := ""
	if end < 0 {
		inner = lx.src[lx.i:]
		lx.i = len(lx.src)
	} else {
		inner = lx.src[lx.i : lx.i+end]
		lx.i += end + 1
	}
	lx.cur.nested = append(lx.cur.nested, inner)
	lx.addExp(expansion{
		kind:   expSubst,
		inner:  inner,
		quoted: quoted,
	}, lx.src[start:lx.i])
}

// captureParens reads up to the parenthesis that closes one already opened,
// skipping quoted text and here-document bodies, and returns what is inside.
func captureParens(src string, i *int) string {
	start := *i
	depth := 1
	var delims []pendingHeredoc
	for *i < len(src) {
		c := src[*i]
		switch c {
		case '\\':
			*i += 2
			continue
		case '\'':
			end := strings.IndexByte(src[*i+1:], '\'')
			if end < 0 {
				*i = len(src)
				continue
			}
			*i += end + 2
			continue
		case '"':
			*i++
			for *i < len(src) && src[*i] != '"' {
				if src[*i] == '\\' {
					*i++
				}
				*i++
			}
			*i++
			continue
		case '<':
			if strings.HasPrefix(src[*i:], "<<") && !strings.HasPrefix(src[*i:], "<<<") {
				*i += 2
				strip := false
				if *i < len(src) && src[*i] == '-' {
					strip = true
					*i++
				}
				sub := &lexer{
					src: src,
					i:   *i,
				}
				delim, _ := sub.readDelimiter()
				delims = append(delims, pendingHeredoc{
					delim: delim,
					strip: strip,
				})
				*i = sub.i
				continue
			}
		case '\n':
			*i++
			for _, d := range delims {
				readHeredocBody(src, i, d.delim, d.strip)
			}
			delims = nil
			continue
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				inner := src[start:*i]
				*i++
				return inner
			}
		}
		*i++
	}
	if start > len(src) {
		*i = len(src)
		return ""
	}
	*i = len(src)
	return src[start:]
}

// resolvePipes gives a command that reads a pipe, and has no redirection of
// its own, the here-document or file that a plain `cat` writes into it. A
// command with both keeps its own and is checked as ambiguous later.
func (lx *lexer) resolvePipes() {
	for i := range lx.line.cmds {
		dst := &lx.line.cmds[i]
		if dst.pipeFrom < 0 || dst.stdinSources > 0 {
			continue
		}
		src := lx.line.cmds[dst.pipeFrom]
		if len(src.args) == 0 || src.args[0] != "cat" || src.pipeFrom >= 0 || src.stdinSources > 1 || anyTrue(src.dyn) {
			continue
		}
		switch {
		case len(src.args) == 1 && src.hasStdin:
			dst.stdin = src.stdin
			dst.hasStdin = true
			dst.stdinDynamic = src.stdinDynamic
		case len(src.args) == 1 && src.stdinFile != "":
			dst.stdinFile = src.stdinFile
			dst.stdinFileDynamic = src.stdinFileDynamic
		case len(src.args) == 2 && src.stdinSources == 0 && !strings.HasPrefix(src.args[1], "-"):
			dst.stdinFile = src.args[1]
		default:
			continue
		}
		dst.pipeFrom = -1
		dst.stdinSources = 1
	}
}

func anyTrue(b []bool) bool {
	for _, v := range b {
		if v {
			return true
		}
	}
	return false
}

// resolvePath resolves p against dir, expanding a leading ~.
func resolvePath(dir, p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	if filepath.IsAbs(p) || dir == "" {
		return p
	}
	return filepath.Join(dir, p)
}
