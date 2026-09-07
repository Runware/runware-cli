package serverless

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildEnvironmentVariables(t *testing.T) {
	got, err := buildEnvironmentVariables(nil, []string{"HF_TOKEN=abc", "LOG_LEVEL=debug"})
	if err != nil {
		t.Fatalf("buildEnvironmentVariables: %v", err)
	}
	if got == nil || len(*got) != 2 || (*got)["HF_TOKEN"] != "abc" {
		t.Errorf("got %v", got)
	}
}

// Nothing supplied must send nil, not an empty map: the field is optional and an
// explicit {} is a different statement from saying nothing.
func TestBuildEnvironmentVariables_NoneIsNil(t *testing.T) {
	got, err := buildEnvironmentVariables(nil, nil)
	if err != nil {
		t.Fatalf("buildEnvironmentVariables: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", *got)
	}
}

// A value may legitimately contain '=' -- base64 padding, a connection string --
// so only the first separator splits.
func TestBuildEnvironmentVariables_ValueMayContainEquals(t *testing.T) {
	got, err := buildEnvironmentVariables(nil, []string{"TOKEN=abc==def="})
	if err != nil {
		t.Fatalf("buildEnvironmentVariables: %v", err)
	}
	if (*got)["TOKEN"] != "abc==def=" {
		t.Errorf("value = %q", (*got)["TOKEN"])
	}
}

// An empty value is a legitimate assignment: unsetting by setting empty is how
// callers override a default the image bakes in.
func TestBuildEnvironmentVariables_EmptyValueAllowed(t *testing.T) {
	got, err := buildEnvironmentVariables(nil, []string{"QUIET="})
	if err != nil {
		t.Fatalf("buildEnvironmentVariables: %v", err)
	}
	if v, ok := (*got)["QUIET"]; !ok || v != "" {
		t.Errorf("got %v", *got)
	}
}

func TestBuildEnvironmentVariables_Rejects(t *testing.T) {
	cases := []struct {
		name string
		pair string
		want string
	}{
		{name: "no separator", pair: "HF_TOKEN", want: "not KEY"},
		{name: "empty name", pair: "=value", want: "empty name"},
		{name: "leading digit", pair: "1BAD=x", want: "POSIX-style"},
		{name: "hyphen", pair: "BAD-NAME=x", want: "POSIX-style"},
		{name: "dot", pair: "bad.name=x", want: "POSIX-style"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildEnvironmentVariables(nil, []string{tc.pair})
			if err == nil {
				t.Fatalf("expected an error for %q", tc.pair)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestBuildEnvironmentVariables_TooLongValue(t *testing.T) {
	_, err := buildEnvironmentVariables(nil, []string{"BIG=" + strings.Repeat("x", maxEnvValueLen+1)})
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected a length error, got %v", err)
	}
}

// The file form exists so a secret never lands in an argv, where it is visible
// in the process list and recorded in shell history.
func TestBuildEnvironmentVariables_FromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env.deploy")
	content := "# a comment\n\nHF_TOKEN=from-file\nexport LOG_LEVEL=debug\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := buildEnvironmentVariables([]string{path}, nil)
	if err != nil {
		t.Fatalf("buildEnvironmentVariables: %v", err)
	}
	if (*got)["HF_TOKEN"] != "from-file" {
		t.Errorf("HF_TOKEN = %q", (*got)["HF_TOKEN"])
	}
	// `export FOO=bar` is what a shell-sourced file looks like; pasting one in
	// should work rather than fail on the keyword.
	if (*got)["LOG_LEVEL"] != "debug" {
		t.Errorf("LOG_LEVEL = %q (export prefix not absorbed)", (*got)["LOG_LEVEL"])
	}
	if len(*got) != 2 {
		t.Errorf("comments or blank lines became entries: %v", *got)
	}
}

// An explicit --env is the more specific statement, so it wins over a file entry
// with the same name.
func TestBuildEnvironmentVariables_InlineOverridesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env.deploy")
	if err := os.WriteFile(path, []byte("LOG_LEVEL=info\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := buildEnvironmentVariables([]string{path}, []string{"LOG_LEVEL=debug"})
	if err != nil {
		t.Fatalf("buildEnvironmentVariables: %v", err)
	}
	if (*got)["LOG_LEVEL"] != "debug" {
		t.Errorf("LOG_LEVEL = %q, want the inline value", (*got)["LOG_LEVEL"])
	}
}

// A malformed line has to name the file and the line, or a long .env is a hunt.
func TestBuildEnvironmentVariables_FileErrorNamesTheLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env.deploy")
	if err := os.WriteFile(path, []byte("GOOD=1\nBROKEN\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := buildEnvironmentVariables([]string{path}, nil)
	if err == nil {
		t.Fatal("expected an error for a malformed line")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error %q does not name the line", err)
	}
}

func TestBuildEnvironmentVariables_MissingFile(t *testing.T) {
	_, err := buildEnvironmentVariables([]string{filepath.Join(t.TempDir(), "nope")}, nil)
	if err == nil {
		t.Fatal("expected an error for a missing env file")
	}
}

func TestBuildEnvironmentVariables_TooMany(t *testing.T) {
	pairs := make([]string, maxEnvVars+1)
	for i := range pairs {
		pairs[i] = "VAR_" + strings.Repeat("a", i%20) + string(rune('A'+i%26)) + string(rune('A'+i/26)) + "=x"
	}
	if _, err := buildEnvironmentVariables(nil, pairs); err == nil {
		t.Fatal("expected an error past the variable limit")
	}
}

// A quoted value in an --env-file must arrive unquoted: shells strip quotes when
// sourcing, and passing them through sends `"hf_x"` as the token itself -- a 401
// in the pod with nothing to read.
func TestBuildEnvironmentVariables_StripsSurroundingQuotes(t *testing.T) {
	cases := []struct {
		name string
		line string
		want string
	}{
		{name: "double", line: `HF_TOKEN="hf_x"`, want: "hf_x"},
		{name: "single", line: `HF_TOKEN='hf_x'`, want: "hf_x"},
		// Only a matching pair, and only when it surrounds: a value that
		// genuinely contains a quote keeps it.
		{name: "unbalanced", line: `HF_TOKEN="hf_x`, want: `"hf_x`},
		{name: "mismatched", line: `HF_TOKEN='hf_x"`, want: `'hf_x"`},
		{name: "inner quote kept", line: `MSG=say "hi"`, want: `say "hi"`},
		{name: "one pair only", line: `MSG=""quoted""`, want: `"quoted"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, envDotfile)
			if err := os.WriteFile(path, []byte(tc.line+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := buildEnvironmentVariables([]string{path}, nil)
			if err != nil {
				t.Fatalf("buildEnvironmentVariables: %v", err)
			}
			for _, v := range *got {
				if v != tc.want {
					t.Errorf("value = %q, want %q", v, tc.want)
				}
			}
		})
	}
}

// Whitespace inside a value is the caller's business: trimming the whole line
// would silently rewrite a token or an indented value.
func TestBuildEnvironmentVariables_PreservesValueWhitespace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, envDotfile)
	// A trailing space in the value, and a leading-space line that still parses.
	if err := os.WriteFile(path, []byte("PADDED=value \n  INDENTED=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := buildEnvironmentVariables([]string{path}, nil)
	if err != nil {
		t.Fatalf("buildEnvironmentVariables: %v", err)
	}
	if (*got)["PADDED"] != "value " {
		t.Errorf("PADDED = %q, want %q", (*got)["PADDED"], "value ")
	}
	if (*got)["INDENTED"] != "x" {
		t.Errorf("INDENTED = %q, want x", (*got)["INDENTED"])
	}
}

// The API's limits are characters; len() counts bytes, so a byte check rejects a
// valid non-ASCII value at half the documented limit.
func TestBuildEnvironmentVariables_LimitIsCountedInRunes(t *testing.T) {
	// 4096 two-byte runes: 4096 characters, 8192 bytes.
	value := strings.Repeat("é", maxEnvValueLen)
	got, err := buildEnvironmentVariables(nil, []string{"MSG=" + value})
	if err != nil {
		t.Fatalf("a value of exactly the character limit was rejected: %v", err)
	}
	if (*got)["MSG"] != value {
		t.Error("value did not survive")
	}
	// One rune past is still rejected.
	if _, err := buildEnvironmentVariables(nil, []string{"MSG=" + value + "é"}); err == nil {
		t.Error("expected a rejection one character past the limit")
	}
}
