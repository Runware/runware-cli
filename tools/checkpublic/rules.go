package main

import (
	_ "embed"
	"net/netip"
	"path"
	"regexp"
	"strings"
	"sync"
)

// allowMarker, followed by a reason on the same line, exempts that line from
// every rule except the credential and deny-list rules (see neverAllowed). Use it for a
// line that has to name a pattern, such as a test of a rule or a guard like
// the published-spec test. A marker without a reason exempts nothing.
const allowMarker = "checkpublic:allow"

// allowed matches the marker followed by a reason: optionally a colon, then
// a space and at least one more character.
var allowed = regexp.MustCompile(regexp.QuoteMeta(allowMarker) + `:?[ \t]+\S`)

// neverAllowed are the rules the allow marker does not exempt: a pasted
// key or token, or a name from the deny list, is a leak whatever the line
// says about it.
var neverAllowed = map[string]struct{}{
	rulePrivateKey:  {},
	ruleTokenPrefix: {},
	ruleCredential:  {},
	ruleBearer:      {},
	ruleMaskedKey:   {},
	ruleDenyList:    {},
}

//go:embed public-repos.txt
var publicReposFile string

// Rule names, as printed in findings.
const (
	ruleRepo         = "private-repo"
	ruleInternalLink = "internal-link"
	ruleInternalHost = "internal-host"
	ruleSpecMarker   = "spec-internal-marker"
	rulePrivateKey   = "private-key"
	ruleBearer       = "bearer-token"
	ruleCredential   = "credential-assignment" //nolint:gosec // a rule name, not a credential
	ruleTokenPrefix  = "token-prefix"
	rulePublicIP     = "public-ip"
	ruleMaskedKey    = "masked-key"
	ruleDenyList     = "deny-list"
	ruleTicketKey    = "ticket-key"
)

// hit is one rule match on one line.
type hit struct {
	rule    string
	message string
	match   string
}

// rule is one pattern. check, when set, filters or explains a match: it gets
// the submatches and the path of the scanned file ("" for text that is not a
// file) and returns false to drop the match.
type rule struct {
	name    string
	message string
	re      *regexp.Regexp
	check   func(s *Scanner, m []string, file string) bool
	// bounded, when set, keeps a match only when line[start:end] stands on
	// its own. It replaces boundary groups in re, which would consume the
	// separator that the next match needs.
	bounded func(line string, start, end int) bool
	// post, when set, applies the rule only to what is posted on GitHub
	// (a PR title, description or comment), not to code, commits or a
	// branch name.
	post bool
}

// Scanner holds the rules and the public repository allowlist.
type Scanner struct {
	publicRepos map[string]struct{}
	rules       []rule
	// loadDeny, when set, returns the deny list: names that must not be
	// published but cannot be written down here either (see deny.go). It
	// runs once, before the first line is scanned, so a run that scans
	// nothing (a push of a deletion, or to a private repository) never
	// fetches the deny list.
	loadDeny func() []*regexp.Regexp
	denyOnce sync.Once
}

// NewScanner builds a scanner with the built-in rules.
func NewScanner() *Scanner {
	return &Scanner{
		publicRepos: parsePublicRepos(publicReposFile),
		rules:       builtinRules(),
	}
}

// denyRules adds the rules of the deny list, once.
func (s *Scanner) denyRules() {
	s.denyOnce.Do(func() {
		if s.loadDeny == nil {
			return
		}
		for _, re := range s.loadDeny() {
			s.rules = append(s.rules, rule{
				name:    ruleDenyList,
				message: "matches a pattern from the deny list",
				re:      re,
			})
		}
	})
}

// listLines returns the entries of a list file: one per line, with blank
// lines and lines starting with # left out.
func listLines(text string) []string {
	var out []string
	for line := range strings.SplitSeq(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, line)
		}
	}
	return out
}

func parsePublicRepos(text string) map[string]struct{} {
	repos := map[string]struct{}{}
	for _, line := range listLines(text) {
		repos[strings.ToLower(line)] = struct{}{}
	}
	return repos
}

// IsPublicRepo reports whether name is in the allowlist of public repositories.
func (s *Scanner) IsPublicRepo(name string) bool {
	_, ok := s.publicRepos[strings.ToLower(name)]
	return ok
}

const repoMessage = "names a Runware repository that is not in tools/checkpublic/public-repos.txt; add it there only if the repository is public, in a change of its own: a new entry takes effect only after that change is merged (CI builds the checker from the base) and make setup is run again"

const hostMessage = "names an internal host"

func builtinRules() []rule {
	return []rule{
		{
			name:    ruleRepo,
			message: repoMessage,
			// A web or clone URL, an API or raw-content URL, or a gh api
			// path: github.com/, api.github.com/repos/,
			// raw.githubusercontent.com/ or repos/ before the owner.
			re:    regexp.MustCompile(`(?i)(?:github\.com[/:]|githubusercontent\.com/|\brepos/)runware/([a-z0-9_.-]+)`),
			check: checkRepo,
		},
		{
			// The short forms: owner/name#123, owner/name/pull/1, or a bare
			// owner/name in prose. @Runware/name is a team, not a
			// repository.
			name:    ruleRepo,
			message: repoMessage,
			re:      regexp.MustCompile(`(?i)(?:^|[^a-z0-9_./@-])runware/([a-z0-9_.-]+)`),
			check:   checkShortRepo,
		},
		{
			name:    ruleInternalLink,
			message: "links to an internal tool (Jira, Notion, Slack, Google Docs or Google Drive)",
			re:      regexp.MustCompile(`(?i)\b(?:[a-z0-9-]+\.)*(?:atlassian\.net|notion\.so|notion\.site|slack\.com|docs\.google\.com|drive\.google\.com)\b`),
		},
		{
			// A Runware ticket key, of the RUNSERV project. A
			// key followed by -slug or / is a branch name, which may carry
			// one.
			name:    ruleTicketKey,
			message: "has a ticket key; a PR title, description or comment explains the change without it, and the key can stay in the branch name",
			re:      regexp.MustCompile(`\bRUNSERV-[0-9]+\b`),
			bounded: notBranchName,
			post:    true,
		},
		{
			name:    ruleInternalHost,
			message: hostMessage,
			re:      regexp.MustCompile(`(?i)\b(?:[a-z0-9-]+\.)*svc\.cluster\.local\b`),
		},
		{
			// The short form of a Kubernetes service host, service.namespace.svc,
			// in a URL or followed by a port. svcBounded keeps a Go
			// selector such as a.b.svc out.
			name:    ruleInternalHost,
			message: hostMessage,
			re:      regexp.MustCompile(`(?i)[a-z0-9-]+(?:\.[a-z0-9-]+)+\.svc`),
			bounded: svcBounded,
		},
		{
			name:    ruleInternalHost,
			message: hostMessage,
			re:      regexp.MustCompile(`(?i)\b[a-z0-9-]+(?:\.[a-z0-9-]+)+\.internal\b`),
			check:   checkInternalTLD,
		},
		{
			// A single-label .internal host only in a URL context: after
			// // or user@, or before a port. s.internal is a field.
			name:    ruleInternalHost,
			message: hostMessage,
			re:      regexp.MustCompile(`(?i)(?://|@)[a-z0-9-]+\.internal(?:[:/?#"'\s)\]>]|$)|(?:^|[^a-z0-9.-])[a-z0-9-]+\.internal:[0-9]`),
		},
		{
			name:    ruleSpecMarker,
			message: "has an OpenAPI marker of an unpublished section; sync the spec with `make sync-serverless-spec`",
			re:      regexp.MustCompile(`\bx-(?:internal\b|runware-[a-z0-9-]+)`),
		},
		{
			name:    rulePrivateKey,
			message: "has a PEM private key",
			re:      regexp.MustCompile(`-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY-----`),
		},
		{
			name:    ruleBearer,
			message: "has an Authorization header with a literal bearer token",
			// The comma is Go's Header.Set("Authorization", "Bearer ...").
			re:    regexp.MustCompile(`(?i)\bauthorization["']?\s*[:=,]\s*["']?bearer\s+([a-z0-9._~+/-]{16,}=*)`),
			check: checkSecretValue(1),
		},
		{
			name:    ruleCredential,
			message: "assigns a literal value to a credential-like name",
			re:      regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:[a-z0-9]+[_-])*(?:api[_-]?key|access[_-]?key|token|secret|passw(?:or)?d)["']?\s*(?::=|=|:)\s*(["'` + "`" + `]?)([a-z0-9._~+/-]{20,}=*)`),
			check:   checkCredential,
		},
		{
			// camelCase and PascalCase names, as Go and JavaScript write
			// them: accessToken, clientSecret, runwareApiKey.
			name:    ruleCredential,
			message: "assigns a literal value to a credential-like name",
			re:      regexp.MustCompile(`(?:^|[^A-Za-z0-9_])[A-Za-z][A-Za-z0-9]*?(?:Api[Kk]ey|APIKey|AccessKey|Token|Secret|Passw(?:or)?d)["']?\s*(?::=|=|:)\s*(["'` + "`" + `]?)([A-Za-z0-9._~+/-]{20,}=*)`),
			check:   checkCredential,
		},
		{
			name:    ruleTokenPrefix,
			message: "has a credential with a known prefix",
			re:      regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{30,}|(?:AKIA|ASIA)[0-9A-Z]{16}|xox[abprs]-[A-Za-z0-9-]{10,})\b`),
		},
		{
			name:    rulePublicIP,
			message: "has a public IPv4 address; use one from a documentation range (192.0.2.0/24, 198.51.100.0/24, 203.0.113.0/24)",
			re:      regexp.MustCompile(`((?:[0-9]{1,3}\.){3}[0-9]{1,3})`),
			check:   checkPublicIP,
			bounded: ipBounded,
		},
		{
			name:    ruleMaskedKey,
			message: "shows the visible prefix of a masked key; the prefix of a real key is still part of it",
			re:      regexp.MustCompile(`[A-Za-z0-9]{6,}[-_]?•{3,}`),
		},
	}
}

func checkRepo(s *Scanner, m []string, _ string) bool {
	name := strings.TrimSuffix(strings.TrimRight(m[1], "."), ".git")
	return name != "" && !s.IsPublicRepo(name)
}

// checkShortRepo is checkRepo that also reads owner/name as Homebrew does,
// for the tap in the homebrew-name repository: brew install runware/tap/x.
func checkShortRepo(s *Scanner, m []string, file string) bool {
	return checkRepo(s, m, file) && !s.IsPublicRepo("homebrew-"+m[1])
}

// notBranchName reports whether the ticket key at line[start:end] is not the
// start of a branch name: RUNSERV-12-fix or RUNSERV-12/fix.
func notBranchName(line string, _, end int) bool {
	return end == len(line) || line[end] != '-' && line[end] != '/'
}

// publicInternalHosts are .internal names that are public knowledge and
// belong to a tool, not to Runware.
var publicInternalHosts = map[string]struct{}{
	"host.docker.internal":    {},
	"gateway.docker.internal": {},
}

func checkInternalTLD(_ *Scanner, m []string, _ string) bool {
	_, ok := publicInternalHosts[strings.ToLower(m[0])]
	return !ok
}

// placeholderWords mark a value as an obvious placeholder rather than a secret.
var placeholderWords = []string{"example", "test", "fake", "dummy", "placeholder", "redacted", "xxxx", "your", "sample", "changeme", "secret"}

// checkSecretValue keeps a match only when submatch i looks like a real
// secret: it has a letter and a digit and is not an obvious placeholder.
func checkSecretValue(i int) func(*Scanner, []string, string) bool {
	return func(_ *Scanner, m []string, _ string) bool {
		return looksSecret(m[i])
	}
}

func looksSecret(v string) bool {
	if !strings.ContainsAny(v, "0123456789") || !strings.ContainsAny(strings.ToLower(v), "abcdefghijklmnopqrstuvwxyz") {
		return false
	}
	lower := strings.ToLower(v)
	for _, w := range placeholderWords {
		if strings.Contains(lower, w) {
			return false
		}
	}
	return true
}

var hexValue = regexp.MustCompile(`^[0-9a-fA-F]+$`)

// checkCredential gets the credential rules' submatches: the opening quote
// of the value, if any, and the value. In a source file only a quoted string
// literal is a candidate, since an unquoted value there is code (a field, a
// call, a variable). Anywhere else, a configuration file or text that is not
// a file, an unquoted value is a literal too.
func checkCredential(_ *Scanner, m []string, file string) bool {
	quote, v := m[1], m[2]
	if quote == "" && isCodeFile(file) {
		return false
	}
	// Golden vectors under testdata are hex keys made for the test.
	if hexValue.MatchString(v) && inTestdata(file) {
		return false
	}
	// A dotted Go or YAML reference (cfg.Section.Name) is not a literal.
	if strings.Contains(v, ".") && !strings.ContainsAny(v, "0123456789") {
		return false
	}
	return looksSecret(v)
}

// codeExtensions are the extensions of programming language source files.
var codeExtensions = map[string]struct{}{
	".go":     {},
	".ts":     {},
	".tsx":    {},
	".mts":    {},
	".cts":    {},
	".js":     {},
	".jsx":    {},
	".mjs":    {},
	".cjs":    {},
	".py":     {},
	".rb":     {},
	".java":   {},
	".kt":     {},
	".kts":    {},
	".scala":  {},
	".groovy": {},
	".rs":     {},
	".swift":  {},
	".c":      {},
	".h":      {},
	".cc":     {},
	".cpp":    {},
	".cxx":    {},
	".hpp":    {},
	".cs":     {},
	".php":    {},
	".pl":     {},
	".lua":    {},
	".dart":   {},
	".ex":     {},
	".exs":    {},
	".zig":    {},
}

// isCodeFile reports whether file is a programming language source file,
// judged by its extension. Text that is not a file has the path "".
func isCodeFile(file string) bool {
	_, ok := codeExtensions[strings.ToLower(path.Ext(file))]
	return ok
}

func inTestdata(file string) bool {
	for dir := range strings.SplitSeq(path.Dir(file), "/") {
		if dir == "testdata" {
			return true
		}
	}
	return false
}

// wellKnownIPs are public resolvers that appear in examples and tests.
var wellKnownIPs = map[string]struct{}{
	"1.1.1.1": {},
	"1.0.0.1": {},
	"8.8.8.8": {},
	"8.8.4.4": {},
	"9.9.9.9": {},
}

var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),   // shared address space
	netip.MustParsePrefix("192.0.0.0/24"),    // protocol assignments
	netip.MustParsePrefix("192.0.2.0/24"),    // documentation
	netip.MustParsePrefix("198.18.0.0/15"),   // benchmarking
	netip.MustParsePrefix("198.51.100.0/24"), // documentation
	netip.MustParsePrefix("203.0.113.0/24"),  // documentation
	netip.MustParsePrefix("240.0.0.0/4"),     // reserved
}

// ipBounded reports whether the dotted quad at line[start:end] is not part of
// a longer word or dotted number: no letter, digit or dot before it, and no
// letter or digit after it, nor a dot followed by a digit.
func ipBounded(line string, start, end int) bool {
	if start > 0 && isWordOrDot(line[start-1]) {
		return false
	}
	if end == len(line) {
		return true
	}
	if line[end] == '.' {
		return end+1 == len(line) || !isDigit(line[end+1])
	}
	return !isWordOrDot(line[end])
}

// svcBounded reports whether the short service host at line[start:end] is
// used as a host: inside a URL (after // or after the user@ of a URL) or
// followed by a port. A bare
// name.namespace.svc is left alone, because it reads the same as a Go
// selector such as h.app.svc.
func svcBounded(line string, start, end int) bool {
	if start > 0 && (isWordOrDot(line[start-1]) || line[start-1] == '-' || line[start-1] == '_') {
		return false
	}
	if end+1 < len(line) && line[end] == ':' && isDigit(line[end+1]) {
		return true
	}
	before := line[:start]
	inURL := strings.HasSuffix(before, "//") ||
		(strings.HasSuffix(before, "@") && strings.Contains(before, "//"))
	if !inURL {
		return false
	}
	if end == len(line) {
		return true
	}
	switch line[end] {
	case ' ', '\t', '"', '\'', '`', '/', '>', ']', ')', ',', ';', '?', '#':
		return true
	case '.':
		return end+1 == len(line) || line[end+1] == ' '
	}
	return false
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}

func isWordOrDot(c byte) bool {
	return isDigit(c) || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '.'
}

func checkPublicIP(_ *Scanner, m []string, _ string) bool {
	addr, err := netip.ParseAddr(m[1])
	if err != nil {
		return false
	}
	if _, ok := wellKnownIPs[m[1]]; ok {
		return false
	}
	if !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return false
	}
	for _, p := range nonPublicPrefixes {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}

// excludedFile reports whether a file is skipped entirely: go.sum, whose
// hashes match the credential rules by chance, and binary files, whose bytes
// the diff prints as text only to match by chance.
func excludedFile(file string) bool {
	base := path.Base(file)
	if _, ok := binaryExtensions[strings.ToLower(path.Ext(base))]; ok {
		return true
	}
	return base == "go.sum"
}

// binaryExtensions are the extensions of files that are binary by format:
// images, archives, fonts and compiled objects.
var binaryExtensions = map[string]struct{}{
	".png":   {},
	".jpg":   {},
	".jpeg":  {},
	".gif":   {},
	".webp":  {},
	".ico":   {},
	".zip":   {},
	".gz":    {},
	".tgz":   {},
	".zst":   {},
	".woff":  {},
	".woff2": {},
	".ttf":   {},
	".otf":   {},
	".o":     {},
	".a":     {},
	".so":    {},
	".dylib": {},
	".dll":   {},
	".exe":   {},
}

// mergeSubject is the subject GitHub writes for a merge commit. Its
// owner/branch names a branch, not a repository, so the repository rules
// read the line without it; every other rule reads the line as written.
var mergeSubject = regexp.MustCompile(`^Merge pull request #[0-9]+ from \S+`)

// submatches turns the indexes of one FindAllStringSubmatchIndex match into
// the strings FindAllStringSubmatch would return.
func submatches(line string, idx []int) []string {
	m := make([]string, len(idx)/2)
	for i := range m {
		if idx[2*i] >= 0 {
			m[i] = line[idx[2*i]:idx[2*i+1]]
		}
	}
	return m
}

// ScanLine returns the rule matches on one line of file ("" for free text).
func (s *Scanner) ScanLine(file, line string) []hit {
	return s.scanLine(file, line, false)
}

// scanLine is ScanLine that also applies the post rules when post is set.
func (s *Scanner) scanLine(file, line string, post bool) []hit {
	// The marker is for a line of a file: a post would publish it.
	allow := !post && allowed.MatchString(line)
	s.denyRules()
	original := line
	repoLine := mergeSubject.ReplaceAllLiteralString(line, "Merge pull request")
	var hits []hit
	seen := map[string]struct{}{}
	for _, r := range s.rules {
		if r.post && !post {
			continue
		}
		if _, never := neverAllowed[r.name]; allow && !never {
			continue
		}
		line := original
		if r.name == ruleRepo {
			line = repoLine
		}
		for _, idx := range r.re.FindAllStringSubmatchIndex(line, -1) {
			if r.bounded != nil && !r.bounded(line, idx[0], idx[1]) {
				continue
			}
			m := submatches(line, idx)
			if r.check != nil && !r.check(s, m, file) {
				continue
			}
			match := strings.TrimLeft(m[0], " \t\"'([<,;:=|")
			if r.name == ruleRepo || r.name == rulePublicIP {
				match = m[1]
			}
			key := r.name + "\x00" + strings.ToLower(match)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			hits = append(hits, hit{
				rule:    r.name,
				message: r.message,
				match:   match,
			})
		}
	}
	return hits
}
