package main

import (
	"os"
	"path/filepath"
	"strings"
)

// shellCmd is one simple command of a shell command line, after quote
// removal, with what it reads on stdin when the command line says so.
type shellCmd struct {
	args []string
	// stdin is the content of a here-document or here-string, or of a
	// `cat <<EOF` or `cat file` piped into the command.
	stdin    string
	hasStdin bool
	// stdinFile is the file of a `< file` redirection.
	stdinFile string
	// pipeFrom is the index of the command piped into this one, or -1.
	pipeFrom int
}

type pendingHeredoc struct {
	delim string
	strip bool
	cmd   int
}

// lexer splits a command line into simple commands. It understands quoting,
// escapes, command separators, pipes, redirections, here-documents and
// command substitution well enough to recover the arguments a hook has to
// check. It does not expand variables: $VAR stays literal.
type lexer struct {
	src  string
	i    int
	dir  string
	cmds []shellCmd

	cur    shellCmd
	word   strings.Builder
	inWord bool

	// Where the next word goes instead of the argument list.
	skipNext       bool
	stdinFileNext  bool
	hereStringNext bool

	heredocs []pendingHeredoc
}

// parseShell parses src. dir resolves the files that `cat file` reads in a
// command substitution.
func parseShell(src, dir string) []shellCmd {
	lx := &lexer{
		src: src,
		dir: dir,
	}
	lx.cur.pipeFrom = -1
	lx.run()
	lx.resolvePipes()
	return lx.cmds
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
			lx.endCmd()
			lx.i++
			lx.readHeredocs()
		case c == ';' || c == '(' || c == ')':
			lx.endWord()
			lx.endCmd()
			lx.i++
		case c == '&':
			if lx.peek(1) == '>' {
				lx.redirectOut()
				continue
			}
			lx.endWord()
			lx.endCmd()
			lx.i++
			if lx.peek(0) == '&' {
				lx.i++
			}
		case c == '|':
			lx.endWord()
			lx.endCmd()
			lx.i++
			if lx.peek(0) == '|' {
				lx.i++
				continue
			}
			if lx.peek(0) == '&' {
				lx.i++
			}
			if len(lx.cmds) > 0 {
				lx.cur.pipeFrom = len(lx.cmds) - 1
			}
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
			lx.inWord = true
			if lx.i+1 < len(lx.src) {
				lx.word.WriteByte(lx.src[lx.i+1])
			}
			lx.i += 2
		case c == '\'':
			lx.inWord = true
			end := strings.IndexByte(lx.src[lx.i+1:], '\'')
			if end < 0 {
				lx.word.WriteString(lx.src[lx.i+1:])
				lx.i = len(lx.src)
				continue
			}
			lx.word.WriteString(lx.src[lx.i+1 : lx.i+1+end])
			lx.i += end + 2
		case c == '"':
			lx.inWord = true
			lx.i++
			lx.doubleQuoted()
		case c == '$' && lx.peek(1) == '(':
			lx.inWord = true
			lx.word.WriteString(lx.substitution())
		case c == '$' && lx.peek(1) == '\'':
			lx.inWord = true
			lx.i += 2
			lx.ansiQuoted()
		case c == '`':
			lx.inWord = true
			lx.word.WriteString(lx.backtick())
		default:
			lx.inWord = true
			lx.word.WriteByte(c)
			lx.i++
		}
	}
	lx.endWord()
	lx.endCmd()
}

func (lx *lexer) peek(n int) byte {
	if lx.i+n < len(lx.src) {
		return lx.src[lx.i+n]
	}
	return 0
}

func (lx *lexer) endWord() {
	if !lx.inWord {
		return
	}
	w := lx.word.String()
	lx.word.Reset()
	lx.inWord = false
	switch {
	case lx.skipNext:
		lx.skipNext = false
	case lx.stdinFileNext:
		lx.stdinFileNext = false
		lx.cur.stdinFile = w
	case lx.hereStringNext:
		lx.hereStringNext = false
		lx.cur.stdin = w + "\n"
		lx.cur.hasStdin = true
	default:
		lx.cur.args = append(lx.cur.args, w)
	}
}

func (lx *lexer) endCmd() {
	if len(lx.cur.args) > 0 || lx.cur.hasStdin || lx.cur.stdinFile != "" {
		lx.cmds = append(lx.cmds, lx.cur)
	}
	lx.cur = shellCmd{pipeFrom: -1}
}

// dropFD drops a word that is only a file descriptor number before a redirection.
func (lx *lexer) dropFD() {
	if lx.inWord && strings.Trim(lx.word.String(), "0123456789") == "" {
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
	lx.skipNext = true
}

func (lx *lexer) redirectIn() {
	lx.dropFD()
	switch {
	case strings.HasPrefix(lx.src[lx.i:], "<<<"):
		lx.i += 3
		lx.hereStringNext = true
	case strings.HasPrefix(lx.src[lx.i:], "<<"):
		lx.i += 2
		strip := false
		if lx.peek(0) == '-' {
			strip = true
			lx.i++
		}
		lx.heredocs = append(lx.heredocs, pendingHeredoc{
			delim: lx.readDelimiter(),
			strip: strip,
			cmd:   len(lx.cmds),
		})
	case strings.HasPrefix(lx.src[lx.i:], "<("):
		// Process substitution: keep it as a literal word.
		lx.inWord = true
		lx.word.WriteString(lx.substitution())
	default:
		lx.i++
		if lx.peek(0) == '&' {
			lx.i++
		}
		lx.stdinFileNext = true
	}
}

// readDelimiter reads a here-document delimiter word, removing its quotes.
func (lx *lexer) readDelimiter() string {
	for lx.i < len(lx.src) && (lx.src[lx.i] == ' ' || lx.src[lx.i] == '\t') {
		lx.i++
	}
	var b strings.Builder
	for lx.i < len(lx.src) {
		c := lx.src[lx.i]
		if c == '\'' || c == '"' || c == '\\' {
			lx.i++
			continue
		}
		if strings.IndexByte(" \t\n;&|<>()", c) >= 0 {
			break
		}
		b.WriteByte(c)
		lx.i++
	}
	return b.String()
}

// readHeredocs reads the bodies of the here-documents started on the line
// that just ended. Each belongs to the command that was open when it started,
// which endCmd has appended by now.
func (lx *lexer) readHeredocs() {
	for _, h := range lx.heredocs {
		body := readHeredocBody(lx.src, &lx.i, h.delim, h.strip)
		if h.cmd < len(lx.cmds) {
			lx.cmds[h.cmd].stdin = body
			lx.cmds[h.cmd].hasStdin = true
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
		case c == '$' && lx.peek(1) == '(':
			lx.word.WriteString(lx.substitution())
		case c == '`':
			lx.word.WriteString(lx.backtick())
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
			default:
				lx.word.WriteByte(next)
			}
			lx.i += 2
		default:
			lx.word.WriteByte(c)
			lx.i++
		}
	}
}

// substitution reads $( ... ) or <( ... ) at lx.i and returns its value when
// it is a plain `cat`, `echo` or `printf` the hook can evaluate, or its source
// text otherwise, so the text is still scanned.
func (lx *lexer) substitution() string {
	start := lx.i
	lx.i += 2
	inner := captureParens(lx.src, &lx.i)
	if lx.src[start] == '<' {
		return lx.src[start:lx.i]
	}
	return evalSubstitution(inner, lx.dir)
}

func (lx *lexer) backtick() string {
	lx.i++
	end := strings.IndexByte(lx.src[lx.i:], '`')
	if end < 0 {
		inner := lx.src[lx.i:]
		lx.i = len(lx.src)
		return evalSubstitution(inner, lx.dir)
	}
	inner := lx.src[lx.i : lx.i+end]
	lx.i += end + 1
	return evalSubstitution(inner, lx.dir)
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
				delims = append(delims, pendingHeredoc{
					delim: sub.readDelimiter(),
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
		return ""
	}
	return src[start:]
}

// evalSubstitution returns the output of a command substitution for the
// forms people use to pass a body: cat with a here-document or a file, echo
// and printf. Anything else returns its source text.
func evalSubstitution(inner, dir string) string {
	cmds := parseShell(inner, dir)
	if len(cmds) != 1 || len(cmds[0].args) == 0 {
		return inner
	}
	c := cmds[0]
	switch c.args[0] {
	case "cat":
		if len(c.args) == 1 && c.hasStdin {
			return strings.TrimRight(c.stdin, "\n")
		}
		if len(c.args) == 2 {
			b, err := os.ReadFile(resolvePath(dir, c.args[1]))
			if err == nil {
				return strings.TrimRight(string(b), "\n")
			}
		}
	case "echo":
		return strings.Join(c.args[1:], " ")
	case "printf":
		if len(c.args) >= 3 && strings.HasPrefix(c.args[1], "%s") {
			return strings.Join(c.args[2:], "")
		}
		if len(c.args) == 2 {
			return c.args[1]
		}
	}
	return inner
}

// resolvePipes gives a command that reads a pipe the here-document or file
// that a plain `cat` writes into it.
func (lx *lexer) resolvePipes() {
	for i := range lx.cmds {
		from := lx.cmds[i].pipeFrom
		if from < 0 || lx.cmds[i].hasStdin {
			continue
		}
		src := lx.cmds[from]
		if len(src.args) == 0 || src.args[0] != "cat" {
			continue
		}
		switch {
		case len(src.args) == 1 && src.hasStdin:
			lx.cmds[i].stdin = src.stdin
			lx.cmds[i].hasStdin = true
		case len(src.args) == 1 && src.stdinFile != "":
			lx.cmds[i].stdinFile = src.stdinFile
		case len(src.args) == 2:
			lx.cmds[i].stdinFile = src.args[1]
		}
	}
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
