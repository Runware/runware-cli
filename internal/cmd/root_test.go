package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRootLongMatchesReadme(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	const prefix = "Run image generation"
	var blurb string
	for line := range strings.SplitSeq(string(readme), "\n") {
		if strings.HasPrefix(line, prefix) {
			blurb = line
			break
		}
	}
	if blurb == "" {
		t.Fatalf("README has no opening description starting with %q", prefix)
	}
	cmd := NewRootCmd(newLogger())
	if !strings.Contains(cmd.Long, blurb) {
		t.Fatalf("root Long drifted from README:\nLong:\n%s\nREADME:\n%s", cmd.Long, blurb)
	}
}

// TestServerlessInitHelpNamesInit pins the text that the Serverless install
// scripts in public/serverless look for. They decide whether an installed CLI
// has init from whether 'runware serverless init --help' prints
// "serverless init", because a CLI from before init prints the help of the
// serverless group and also exits 0.
func TestServerlessInitHelpNamesInit(t *testing.T) {
	cmd := NewRootCmd(newLogger())
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"serverless",
		"init",
		"--help",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("serverless init --help: %v\n%s", err, stderr.String())
	}
	if want := "runware serverless init"; !strings.Contains(stdout.String(), want) {
		t.Errorf("stdout does not contain %q:\n%s", want, stdout.String())
	}
}
