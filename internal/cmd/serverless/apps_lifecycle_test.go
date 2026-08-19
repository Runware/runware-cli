package serverless

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/runware/runware-cli/internal/api/transport"
)

const testDeleteKey = "k"

func TestConfirmDelete_Skip(t *testing.T) {
	var out bytes.Buffer
	if err := confirmDelete(testAppID, true, strings.NewReader(""), &out, false, testDeleteKey); err != nil {
		t.Fatalf("confirmDelete: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected no prompt when skipped, got %q", out.String())
	}
}

func TestConfirmDelete_NoAPIKeySkipsPrompt(t *testing.T) {
	var out bytes.Buffer
	err := confirmDelete(testAppID, true, strings.NewReader("y\n"), &out, true, "")
	if !errors.Is(err, transport.ErrNoAPIKey) {
		t.Fatalf("expected ErrNoAPIKey, got %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("expected no prompt without API key, got %q", out.String())
	}
}

func TestConfirmDelete_NonTTYRequiresYes(t *testing.T) {
	err := confirmDelete(testAppID, false, strings.NewReader("y\n"), io.Discard, false, testDeleteKey)
	if !errors.Is(err, errDeleteNeedsConfirm) {
		t.Fatalf("expected errDeleteNeedsConfirm, got %v", err)
	}
}

func TestConfirmDelete_AcceptsYes(t *testing.T) {
	cases := []string{"y\n", "Y\n", "yes\n", "Yes\n", " yes \n"}
	for _, input := range cases {
		var out bytes.Buffer
		if err := confirmDelete(testAppID, false, strings.NewReader(input), &out, true, testDeleteKey); err != nil {
			t.Fatalf("input %q: %v", input, err)
		}
		if !strings.Contains(out.String(), testAppID) {
			t.Fatalf("input %q: expected prompt to mention app id, got %q", input, out.String())
		}
	}
}

func TestConfirmDelete_RejectsNo(t *testing.T) {
	cases := []string{"n\n", "no\n", "\n", "maybe\n"}
	for _, input := range cases {
		err := confirmDelete(testAppID, false, strings.NewReader(input), io.Discard, true, testDeleteKey)
		if !errors.Is(err, errDeleteCancelled) {
			t.Fatalf("input %q: expected errDeleteCancelled, got %v", input, err)
		}
	}
}

func TestConfirmDelete_EOFCancels(t *testing.T) {
	err := confirmDelete(testAppID, false, strings.NewReader(""), io.Discard, true, testDeleteKey)
	if !errors.Is(err, errDeleteCancelled) {
		t.Fatalf("expected errDeleteCancelled, got %v", err)
	}
}

func TestStdinIsTerminal_NonFile(t *testing.T) {
	if stdinIsTerminal(strings.NewReader("y\n")) {
		t.Fatal("non-file reader should not be treated as a TTY")
	}
}

func TestDeleteCmd_SkipFlags(t *testing.T) {
	cases := []struct {
		args  []string
		yes   bool
		force bool
	}{
		{
			args: []string{"--yes"},
			yes:  true,
		},
		{
			args: []string{"-y"},
			yes:  true,
		},
		{
			args:  []string{"--force"},
			force: true,
		},
	}
	for _, tc := range cases {
		cmd := newAppsDeleteCmd(nil)
		if err := cmd.ParseFlags(tc.args); err != nil {
			t.Fatalf("%v: ParseFlags: %v", tc.args, err)
		}
		yes, err := cmd.Flags().GetBool("yes")
		if err != nil {
			t.Fatalf("%v: yes: %v", tc.args, err)
		}
		force, err := cmd.Flags().GetBool("force")
		if err != nil {
			t.Fatalf("%v: force: %v", tc.args, err)
		}
		if yes != tc.yes || force != tc.force {
			t.Fatalf("%v: yes=%v force=%v, want yes=%v force=%v", tc.args, yes, force, tc.yes, tc.force)
		}
	}
}
