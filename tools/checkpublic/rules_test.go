package main

import (
	"fmt"
	"strings"
	"testing"
)

// The positive cases are built at run time from pieces, so this file never
// holds a line the scanner would flag: the scanner checks its own source too.
var (
	ghRepo      = "github.com/" + "Runware/"
	shortRepo   = "Runware" + "/"
	jiraHost    = "example.atlassian" + ".net"
	clusterHost = "api.ns.svc.cluster" + ".local"
	specMarker  = "x-" + "internal"
	pemHeader   = "-----BEGIN " + "PRIVATE KEY-----"
	longSecret  = strings.Repeat("a1B2", 8)
)

type ruleCase struct {
	name string
	file string
	line string
	want []string
}

func TestScanLine(t *testing.T) {
	cases := []ruleCase{
		// private-repo
		{
			name: "private repo link",
			line: "see https://" + ghRepo + "some-private-repo/blob/main/x.md",
			want: []string{ruleRepo},
		},
		{
			name: "private repo link in lowercase",
			line: "github.com/" + "runware/" + "some-private-repo",
			want: []string{ruleRepo},
		},
		{
			name: "private repo ssh remote",
			line: "git@github.com:" + "Runware/" + "some-private-repo.git",
			want: []string{ruleRepo},
		},
		{
			name: "private repo short issue form",
			line: "fixed in " + shortRepo + "some-private-repo#123",
			want: []string{ruleRepo},
		},
		{
			name: "private repo short pull form",
			line: "(" + shortRepo + "some-private-repo/pull/7)",
			want: []string{ruleRepo},
		},
		{
			name: "private repo bare in prose",
			line: "copied from " + shortRepo + "some-private-repo at abc1234.",
			want: []string{ruleRepo},
		},
		{
			name: "public repo",
			line: "https://github.com/Runware/runware-cli/pull/1 and Runware/sdk-go#2",
			want: nil,
		},
		{
			name: "public repo with .git suffix",
			line: "scoop bucket add runware https://github.com/Runware/scoop-bucket.git",
			want: nil,
		},
		{
			name: "go module path",
			line: `"github.com/runware/runware-cli/internal/api"`,
			want: nil,
		},
		{
			name: "team mention",
			line: "* @Runware/dev-admins",
			want: nil,
		},
		{
			name: "merge commit subject",
			line: "Merge pull request #4 from " + shortRepo + "fix/some-branch",
			want: nil,
		},
		// internal-link
		{
			name: "jira link",
			line: "[the bug](https://" + jiraHost + "/browse/ABC-1)",
			want: []string{ruleInternalLink},
		},
		{
			name: "notion link",
			line: "https://www.notion" + ".so/page-123",
			want: []string{ruleInternalLink},
		},
		{
			name: "notion site link",
			line: "https://team.notion" + ".site/page",
			want: []string{ruleInternalLink},
		},
		{
			name: "slack archive link",
			line: "https://team.slack" + ".com/archives/C123/p456",
			want: []string{ruleInternalLink},
		},
		{
			name: "google doc link",
			line: "https://docs.google" + ".com/document/d/abc",
			want: []string{ruleInternalLink},
		},
		{
			name: "bare jira key",
			line: "feat(serverless): add the thing (ABC-1234)",
			want: nil,
		},
		{
			name: "google search link",
			line: "https://www.google.com/search?q=runware",
			want: nil,
		},
		// internal-host
		{
			name: "cluster service",
			line: "http://api.ns.svc.cluster" + ".local:8080",
			want: []string{ruleInternalHost},
		},
		{
			name: "internal tld",
			line: "postgres://db.prod" + ".internal:5432",
			want: []string{ruleInternalHost},
		},
		{
			name: "docker host",
			line: "http://host.docker.internal:8080",
			want: nil,
		},
		{
			name: "go internal package",
			line: `"github.com/runware/runware-cli/internal/config"`,
			want: nil,
		},
		{
			name: "field named internal",
			line: "if s.internal {",
			want: nil,
		},
		{
			name: "public site",
			line: "https://my.runware.ai/signup",
			want: nil,
		},
		// spec-internal-marker
		{
			name: "internal marker",
			line: "      " + specMarker + ": true",
			want: []string{ruleSpecMarker},
		},
		{
			name: "x-runware extension",
			line: "      x-" + "runware-rollout: beta",
			want: []string{ruleSpecMarker},
		},
		{
			name: "other extension",
			line: "      x-go-type: string",
			want: nil,
		},
		// private-key
		{
			name: "pem private key",
			line: pemHeader,
			want: []string{rulePrivateKey},
		},
		{
			name: "pem rsa private key",
			line: "-----BEGIN " + "RSA PRIVATE KEY-----",
			want: []string{rulePrivateKey},
		},
		{
			name: "pem public key",
			line: "-----BEGIN PUBLIC KEY-----",
			want: nil,
		},
		// bearer-token
		{
			name: "authorization header",
			line: "curl -H 'Authorization: " + "Bearer " + longSecret + "'",
			want: []string{ruleBearer},
		},
		{
			name: "authorization header in json",
			line: `"Authorization": ` + `"Bearer ` + longSecret + `"`,
			want: []string{ruleBearer},
		},
		{
			name: "authorization header with variable",
			line: `curl -H "Authorization: Bearer $RUNWARE_API_KEY"`,
			want: nil,
		},
		{
			name: "authorization header with placeholder",
			line: "Authorization: Bearer YOUR_API_KEY_GOES_HERE",
			want: nil,
		},
		// credential-assignment
		{
			name: "api_key assignment",
			line: "api_key" + ": " + longSecret,
			want: []string{ruleCredential},
		},
		{
			name: "env style api key",
			line: "export RUNWARE_API_KEY" + "=" + longSecret,
			want: []string{ruleCredential},
		},
		{
			name: "quoted token",
			line: `token` + ` = "` + longSecret + `"`,
			want: []string{ruleCredential},
		},
		{
			name: "json secret",
			line: `"client_secret"` + `: "` + longSecret + `"`,
			want: []string{ruleCredential},
		},
		{
			name: "short value",
			line: `token = "abc123"`,
			want: nil,
		},
		{
			name: "go reference",
			line: "apiKey := cfg.Defaults.APIKeyFromEnvironment",
			want: nil,
		},
		{
			name: "workflow secret",
			line: "GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}",
			want: nil,
		},
		{
			name: "placeholder",
			line: `APIKey: "secret-key-1234567890abcdef",`,
			want: nil,
		},
		{
			name: "max tokens",
			line: "max_tokens: 100000000000000000000",
			want: nil,
		},
		{
			name: "hex vector under testdata",
			file: "pkg/x/testdata/vectors.json",
			line: `"secret"` + `: "00112233445566778899aabbccddeeff"`,
			want: nil,
		},
		{
			name: "hex value outside testdata",
			file: "pkg/x/config.json",
			line: `"secret"` + `: "00112233445566778899aabbccddeeff"`,
			want: []string{ruleCredential},
		},
		// token-prefix
		{
			name: "github token",
			line: "gh" + "p_" + strings.Repeat("A1b2", 9),
			want: []string{ruleTokenPrefix},
		},
		{
			name: "github fine-grained token",
			line: "github" + "_pat_" + strings.Repeat("A1b2", 9),
			want: []string{ruleTokenPrefix},
		},
		{
			name: "aws access key id",
			line: "AK" + "IA" + "ABCDEFGHIJ234567",
			want: []string{ruleTokenPrefix},
		},
		// public-ip
		{
			name: "public ip",
			line: fmt.Sprintf(`"xForwardedFor": "%d.%d.%d.%d"`, 84, 20, 30, 40),
			want: []string{rulePublicIP},
		},
		{
			name: "private and documentation ips",
			line: "10.0.0.1 192.168.1.1 172.16.0.1 127.0.0.1 192.0.2.10 198.51.100.7 203.0.113.9 100.64.0.1",
			want: nil,
		},
		{
			name: "public resolvers",
			line: "nameserver 1.1.1.1 and 8.8.8.8",
			want: nil,
		},
		{
			name: "dotted version",
			line: "oid 1.3.6.1.4.1 and v1.2.3.4 and 2.27.1.4.5",
			want: nil,
		},
		// masked-key
		{
			name: "masked key with real prefix",
			line: "│ API Key │ " + "abcdEF1234ghIJ56-" + strings.Repeat("•", 5) + " │",
			want: []string{ruleMaskedKey},
		},
		{
			name: "mask constant",
			line: `MaskedKeySuffix = "•••••"`,
			want: nil,
		},
		// allow mechanism
		{
			name: "allow marker",
			line: specMarker + " // " + "checkpublic:" + "allow: the guard names it",
			want: nil,
		},
	}
	s, err := NewScanner(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, h := range s.ScanLine(tc.file, tc.line) {
				got = append(got, h.rule)
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("ScanLine(%q) rules = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
}

func TestLocalDenyList(t *testing.T) {
	s, err := NewScanner([]string{`(?i)\bacme storage\b`})
	if err != nil {
		t.Fatal(err)
	}
	hits := s.ScanLine("", "objects live in ACME Storage")
	if len(hits) != 1 || hits[0].rule != ruleDenyList {
		t.Fatalf("hits = %+v, want one %s", hits, ruleDenyList)
	}
	if _, err := NewScanner([]string{"("}); err == nil {
		t.Fatal("an invalid deny pattern was accepted")
	}
}

func TestExcludedFiles(t *testing.T) {
	s, err := NewScanner(nil)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte(specMarker + "\n")
	for _, file := range []string{"go.sum", "internal/api/gen/client.gen.go"} {
		if f := s.ScanFile(file, content); len(f) != 0 {
			t.Errorf("ScanFile(%s) = %+v, want no findings", file, f)
		}
	}
	if f := s.ScanFile("api/openapi.yaml", content); len(f) != 1 || f[0].Where != "api/openapi.yaml:1" {
		t.Errorf("ScanFile(api/openapi.yaml) = %+v, want one finding on line 1", f)
	}
	if f := s.ScanFile("bin/x", []byte("\x00"+specMarker)); len(f) != 0 {
		t.Errorf("binary file findings = %+v", f)
	}
}

func TestPublicReposAllowlist(t *testing.T) {
	s, err := NewScanner(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"runware-cli", "Runware-CLI", "homebrew-tap", "runware-serverless"} {
		if !s.IsPublicRepo(name) {
			t.Errorf("%s is not in the allowlist", name)
		}
	}
	if s.IsPublicRepo("#") || s.IsPublicRepo("") {
		t.Error("the allowlist header or an empty line counts as a repository")
	}
}

func TestScanDiff(t *testing.T) {
	s, err := NewScanner(nil)
	if err != nil {
		t.Fatal(err)
	}
	diff := strings.Join([]string{
		commitMarker + "0123456789abcdef",
		"diff --git a/a.yaml b/a.yaml",
		"--- a/a.yaml",
		"+++ b/a.yaml",
		"@@ -10,0 +11,2 @@",
		"+fine",
		"+" + specMarker + ": true",
		"-" + specMarker + ": removed lines are not published again",
		"diff --git a/old.txt b/old.txt",
		"--- a/old.txt",
		"+++ /dev/null",
		"@@ -1 +0,0 @@",
		"-" + specMarker,
		"diff --git a/c.md b/c.md",
		"--- /dev/null",
		"+++ b/c.md",
		"@@ -0,0 +1,2 @@",
		"+++ " + specMarker + " is a line of content, not a header",
		"+ok",
		"",
	}, "\n")
	f := s.ScanDiff(diff)
	var where []string
	for _, fd := range f {
		where = append(where, fd.Where)
	}
	want := "commit 0123456789 a.yaml:12,commit 0123456789 c.md:1"
	if strings.Join(where, ",") != want {
		t.Fatalf("ScanDiff findings at %v, want %s", where, want)
	}
}

func TestScanText(t *testing.T) {
	s, err := NewScanner(nil)
	if err != nil {
		t.Fatal(err)
	}
	f := s.ScanText("PR body", "first line\nsee "+ghRepo+"some-private-repo")
	if len(f) != 1 || f[0].Where != "PR body line 2" || f[0].Match != "some-private-repo" {
		t.Fatalf("ScanText = %+v", f)
	}
	f = s.ScanText("PR title", "fix: one line "+clusterHost)
	if len(f) != 1 || f[0].Where != "PR title" {
		t.Fatalf("ScanText = %+v", f)
	}
}
