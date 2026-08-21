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
