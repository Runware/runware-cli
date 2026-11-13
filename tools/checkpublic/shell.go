package main

import (
	"os"
	"path/filepath"
	"strings"
)

// redirTarget is the file of an output redirection.
type redirTarget struct {
	path string
	// dynamic is set when the name holds an expansion, so it could be any
	// file.
	dynamic bool
}

// shellCmd is one simple command of a shell command line, after quote
// removal.
type shellCmd struct {
	args []string
	// dyn[i] is set when args[i] holds a variable, command substitution or
	// glob: the shell may pass anything.
	dyn []bool
	// subs[i] are the inner texts of the substitutions in args[i]; "" stands
	// for any other expansion.
	subs [][]string
	// stdin is the body of a here-document or here-string.
	stdin    string
	hasStdin bool
	// stdinDynamic is set when that body expands variables or commands.
	stdinDynamic bool
	// stdinFile is the file of a `< file` redirection.
	stdinFile        string
	stdinFileDynamic bool
	// stdinSources counts the redirections that feed stdin: <, << and <<<.
	stdinSources int
	// pipeFrom is the index of the command piped into this one, or -1.
	pipeFrom int
	writes   []redirTarget
	// nested are the sources of the command and process substitutions in the
	// command's words, which the shell runs before the command.
	nested []string
	// opaque are here-document bodies that expand commands.
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
// escapes, separators, pipes, redirections, here-documents and substitutions
// well enough to find the commands that publish. It evaluates nothing: a word
// with an expansion keeps its source text and is marked dynamic.
type lexer struct {
	src  string
	i    int
	line shellLine
	cur  shellCmd

	word     strings.Builder
	inWord   bool
	wordDyn  bool
	wordSubs []string
	// wordBrace is set when the word has an unquoted {, which with a comma
	// or .. is a brace expansion; wordTilde when it starts with an unquoted ~.
	wordBrace bool
	wordTilde bool

	// Where the next word goes instead of the argument list.
	writeNext      bool
	stdinFileNext  bool
	hereStringNext bool

	heredocs []pendingHeredoc
}

func parseShell(src string) shellLine {
	lx := &lexer{
		src: src,
	}
	lx.cur.pipeFrom = -1
	lx.run()
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
		case c == '&' && lx.peek(1) == '>':
			lx.redirectOut()
		case c == '&' || c == '|':
			lx.endWord()
			op := string(c)
			lx.i++
			if n := lx.peek(0); n == c || c == '|' && n == '&' {
				op += string(n)
				lx.i++
			}
			lx.endCmd(op)
			if (op == "|" || op == "|&") && len(lx.line.cmds) > 0 {
				lx.cur.pipeFrom = len(lx.line.cmds) - 1
			}
		case (c == '<' || c == '>') && lx.peek(1) == '(':
			lx.inWord = true
			src, _ := lx.substitution()
			lx.addExp("", src)
		case c == '<':
			lx.redirectIn()
		case c == '>':
			lx.redirectOut()
		case c == '#' && !lx.inWord:
			for lx.i < len(lx.src) && lx.src[lx.i] != '\n' {
				lx.i++
			}
		case c == '\\':
			if lx.peek(1) != '\n' {
				lx.inWord = true
				if lx.i+1 < len(lx.src) {
					lx.word.WriteByte(lx.src[lx.i+1])
				}
			}
			lx.i += 2
		case c == '\'':
			lx.inWord = true
			end := strings.IndexByte(lx.src[lx.i+1:], '\'')
			if end < 0 {
				end = len(lx.src) - lx.i - 1
			}
			lx.word.WriteString(lx.src[lx.i+1 : lx.i+1+end])
			lx.i += end + 2
		case c == '"':
			lx.inWord = true
			lx.i++
			lx.doubleQuoted()
		case c == '$' && lx.peek(1) == '\'':
			// $'...' with its escapes is not decoded: an expansion.
			lx.inWord = true
			start := lx.i
			for lx.i += 2; lx.i < len(lx.src) && lx.src[lx.i] != '\''; lx.i++ {
				if lx.src[lx.i] == '\\' {
					lx.i++
				}
			}
			lx.i = min(lx.i+1, len(lx.src))
			lx.addExp("", lx.src[start:lx.i])
		case c == '$' && lx.expands():
			lx.inWord = true
			lx.dollar()
		case c == '`':
			lx.inWord = true
			lx.backtick()
		default:
			if c == '~' && !lx.inWord {
				lx.wordTilde = true
			}
			lx.inWord = true
			switch c {
			case '*', '?', '[':
				lx.addExp("", string(c))
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

// expands reports whether the $ at lx.i starts an expansion.
func (lx *lexer) expands() bool {
	n := lx.peek(1)
	return n == '(' || n == '{' || isNameByte(n) || strings.IndexByte("@*#?$!-", n) >= 0
}

// addExp appends src, the source text of an expansion, to the word. sub is
// the inner text of a substitution, or "" for any other expansion.
func (lx *lexer) addExp(sub, src string) {
	lx.wordDyn = true
	lx.wordSubs = append(lx.wordSubs, sub)
	lx.word.WriteString(src)
}

// dollar reads an expansion at lx.i and keeps its source text.
func (lx *lexer) dollar() {
	if lx.peek(1) == '(' {
		src, inner := lx.substitution()
		if strings.HasPrefix(inner, "(") {
			inner = "" // $(( arithmetic ))
		}
		lx.addExp(inner, src)
		return
	}
	start := lx.i
	lx.i++
	if lx.peek(0) == '{' {
		lx.braced()
	} else {
		lx.i++
		for lx.i < len(lx.src) && isNameByte(lx.src[lx.i]) && !isDigit(lx.src[start+1]) {
			lx.i++
		}
	}
	lx.addExp("", lx.src[start:lx.i])
}

// braced reads a ${ ... } parameter expansion from its { at lx.i to past its
// closing }. The command substitutions in it (${x:-$(cmd)}) run when it
// expands, so they are recorded as nested command lines.
func (lx *lexer) braced() {
	depth := 0
	for lx.i < len(lx.src) {
		switch c := lx.src[lx.i]; {
		case c == '\\':
			lx.i += 2
			continue
		case c == '$' && lx.peek(1) == '(':
			lx.substitution()
			continue
		case c == '$' && lx.peek(1) == '{':
			depth++
			lx.i += 2
			continue
		case c == '`':
			lx.i++
			end := strings.IndexByte(lx.src[lx.i:], '`')
			if end < 0 {
				end = len(lx.src) - lx.i
			}
			lx.cur.nested = append(lx.cur.nested, lx.src[lx.i:lx.i+end])
			lx.i = min(lx.i+end+1, len(lx.src))
			continue
		case c == '{':
			depth++
		case c == '}':
			depth--
		}
		lx.i++
		if depth == 0 {
			return
		}
	}
}

func isNameByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || isDigit(c)
}

func (lx *lexer) endWord() {
	if !lx.inWord {
		return
	}
	w := lx.word.String()
	dyn := lx.wordDyn
	subs := lx.wordSubs
	if lx.wordBrace && (strings.Contains(w, ",") || strings.Contains(w, "..")) ||
		lx.wordTilde && w != "~" && !strings.HasPrefix(w, "~/") {
		dyn = true
		subs = append(subs, "")
	}
	lx.word.Reset()
	lx.inWord, lx.wordDyn, lx.wordBrace, lx.wordTilde = false, false, false, false
	lx.wordSubs = nil
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
		lx.cur.subs = append(lx.cur.subs, subs)
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

// dropFD drops a word that is only a file descriptor number before a
// redirection.
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
	isFD := func() bool {
		return lx.i < len(lx.src) && (isDigit(lx.src[lx.i]) || lx.src[lx.i] == '-')
	}
	if lx.src[lx.i-1] == '&' && isFD() {
		for isFD() {
			lx.i++
		}
		return
	}
	lx.writeNext = true
}

func (lx *lexer) redirectIn() {
	lx.dropFD()
	lx.cur.stdinSources++
	switch {
	case strings.HasPrefix(lx.src[lx.i:], "<<<"):
		lx.i += 3
		lx.hereStringNext = true
	case strings.HasPrefix(lx.src[lx.i:], "<<"):
		lx.i += 2
		strip := lx.peek(0) == '-'
		if strip {
			lx.i++
		}
		delim, quoted := lx.readDelimiter()
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
	for ; lx.i < len(lx.src); lx.i++ {
		c := lx.src[lx.i]
		if c == '\'' || c == '"' || c == '\\' {
			quoted = true
			continue
		}
		if strings.IndexByte(" \t\n;&|<>()", c) >= 0 {
			break
		}
		b.WriteByte(c)
	}
	return b.String(), quoted
}

// readHeredocs reads the bodies of the here-documents started on the line
// that just ended.
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
		if end < 0 {
			end = len(src) - *i
		}
		line := src[*i : *i+end]
		*i = min(*i+end+1, len(src))
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
			switch next := lx.src[lx.i+1]; next {
			case '"', '\\', '$', '`':
				lx.word.WriteByte(next)
			case '\n':
			default:
				lx.word.WriteByte(c)
				lx.word.WriteByte(next)
			}
			lx.i += 2
		case c == '$' && lx.expands():
			lx.dollar()
		case c == '`':
			lx.backtick()
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
func (lx *lexer) backtick() {
	start := lx.i
	lx.i++
	end := strings.IndexByte(lx.src[lx.i:], '`')
	if end < 0 {
		end = len(lx.src) - lx.i
	}
	inner := lx.src[lx.i : lx.i+end]
	lx.i = min(lx.i+end+1, len(lx.src))
	lx.cur.nested = append(lx.cur.nested, inner)
	lx.addExp(inner, lx.src[start:lx.i])
}

// captureParens reads up to the parenthesis that closes one already opened,
// skipping quoted text and here-document bodies, and returns what is inside.
func captureParens(src string, i *int) string {
	start := min(*i, len(src))
	depth := 1
	var delims []pendingHeredoc
	for *i < len(src) {
		switch src[*i] {
		case '\\':
			*i++
		case '\'':
			end := strings.IndexByte(src[*i+1:], '\'')
			if end < 0 {
				end = len(src)
			}
			*i += end + 1
		case '"':
			for *i++; *i < len(src) && src[*i] != '"'; *i++ {
				if src[*i] == '\\' {
					*i++
				}
			}
		case '<':
			if strings.HasPrefix(src[*i:], "<<") && !strings.HasPrefix(src[*i:], "<<<") {
				*i += 2
				strip := *i < len(src) && src[*i] == '-'
				if strip {
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
				*i++
				return src[start : *i-1]
			}
		}
		*i++
	}
	*i = len(src)
	return src[start:]
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
