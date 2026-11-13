package main

import (
	"bufio"
	_ "embed"
	"fmt"
	"net/netip"
	"path"
	"regexp"
	"strings"
	"sync"
)

// allowMarker on a line exempts that line from every rule. Use it for a line
// that has to name a pattern, such as a test of a rule or a guard like the
// published-spec test, and say why in the same comment.
const allowMarker = "checkpublic:allow"

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
}

// Scanner holds the rules and the public repository allowlist.
type Scanner struct {
	publicRepos map[string]struct{}
	rules       []rule
	// loadDeny, when set, returns more deny patterns. It runs once, before
	// the first line is scanned, so a run that scans nothing (a hook call
	// for a command that publishes nothing) never fetches the deny list.
	loadDeny func() []string
	denyOnce sync.Once
}

// NewScanner builds a scanner with the built-in rules plus deny, a list of
// extra regular expressions kept outside the repository (for example names
// that must not be published but cannot be written down here either).
func NewScanner(deny []string) (*Scanner, error) {
	s := &Scanner{
		publicRepos: parsePublicRepos(publicReposFile),
		rules:       builtinRules(),
	}
	for _, d := range deny {
		re, err := regexp.Compile(d)
		if err != nil {
			return nil, fmt.Errorf("deny pattern %q: %w", d, err)
		}
		s.addDeny(re)
	}
	return s, nil
}

func (s *Scanner) addDeny(re *regexp.Regexp) {
	s.rules = append(s.rules, rule{
		name:    ruleDenyList,
		message: "matches a pattern from the deny list",
		re:      re,
	})
}

// denyRules runs loadDeny once. Patterns that do not compile are skipped:
// the loader has already warned about them.
func (s *Scanner) denyRules() {
	s.denyOnce.Do(func() {
		if s.loadDeny == nil {
			return
		}
		for _, d := range s.loadDeny() {
			if re, err := regexp.Compile(d); err == nil {
				s.addDeny(re)
			}
		}
	})
}

func parsePublicRepos(text string) map[string]struct{} {
	repos := map[string]struct{}{}
	sc := bufio.NewScanner(strings.NewReader(text))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		repos[strings.ToLower(line)] = struct{}{}
	}
	return repos
}

// IsPublicRepo reports whether name is in the allowlist of public repositories.
func (s *Scanner) IsPublicRepo(name string) bool {
	_, ok := s.publicRepos[strings.ToLower(name)]
	return ok
}

const repoMessage = "names a Runware repository that is not in tools/checkpublic/public-repos.txt; add it there only if the repository is public"

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
			// The short forms (owner/name#123, owner/name/pull/1, or a bare
			// owner/name in prose) are case sensitive: the lowercase form
			// is the Go module path, and it is always followed by an
			// allowlisted name anyway. @Runware/name is a team, not a
			// repository.
			name:    ruleRepo,
			message: repoMessage,
			re:      regexp.MustCompile(`(?:^|[^A-Za-z0-9_./@-])Runware/([A-Za-z0-9_.-]+)`),
			check:   checkRepo,
		},
		{
			name:    ruleInternalLink,
			message: "links to an internal tool (Jira, Notion, Slack or Google Docs); bare ticket keys are fine, links are not",
			re:      regexp.MustCompile(`(?i)\b(?:[a-z0-9-]+\.)*(?:atlassian\.net|notion\.so|notion\.site|slack\.com|docs\.google\.com)\b`),
		},
		{
			name:    ruleInternalHost,
			message: "names an internal host",
			re:      regexp.MustCompile(`(?i)\b(?:[a-z0-9-]+\.)*svc\.cluster\.local\b`),
		},
		{
			name:    ruleInternalHost,
			message: "names an internal host",
			re:      regexp.MustCompile(`(?i)\b[a-z0-9-]+(?:\.[a-z0-9-]+)+\.internal\b`),
			check:   checkInternalTLD,
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
			re:      regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(?:[a-z0-9]+[_-])*(?:api[_-]?key|access[_-]?key|token|secret|passw(?:or)?d)["']?\s*(?::=|=|:)\s*["']?([a-z0-9._~+/-]{20,}=*)`),
			check:   checkCredential,
		},
		{
			// camelCase and PascalCase names, as Go and JavaScript write
			// them: accessToken, clientSecret, runwareApiKey.
			name:    ruleCredential,
			message: "assigns a literal value to a credential-like name",
			re:      regexp.MustCompile(`(?:^|[^A-Za-z0-9_])[A-Za-z][A-Za-z0-9]*?(?:Api[Kk]ey|APIKey|AccessKey|Token|Secret|Passw(?:or)?d)["']?\s*(?::=|=|:)\s*["']?([A-Za-z0-9._~+/-]{20,}=*)`),
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

func checkCredential(_ *Scanner, m []string, file string) bool {
	v := m[1]
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

// excludedFile reports whether a file is skipped entirely: checksums and
// generated code, whose content comes from files that are scanned themselves.
func excludedFile(file string) bool {
	base := path.Base(file)
	return base == "go.sum" || strings.HasSuffix(base, ".gen.go")
}

// mergeSubject is the subject GitHub writes for a merge commit. Its
// owner/branch names a branch, not a repository, and the branch name is
// checked on its own when it is pushed.
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
	if strings.Contains(line, allowMarker) {
		return nil
	}
	s.denyRules()
	line = mergeSubject.ReplaceAllLiteralString(line, "Merge pull request")
	var hits []hit
	seen := map[string]struct{}{}
	for _, r := range s.rules {
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
