package output

import (
	"testing"
)

func TestNewSpinner_NonTTY(t *testing.T) {
	s := newSpinner(" Generating...", false)
	if s.s != nil {
		t.Fatal("expected nil inner spinner for non-TTY")
	}
}

func TestNewSpinner_TTY(t *testing.T) {
	s := newSpinner(" Generating...", true)
	if s.s == nil {
		t.Fatal("expected non-nil inner spinner for TTY")
	}
}

func TestTTYSpinner_NoopWhenNonTTY(t *testing.T) {
	s := newSpinner(" Generating...", false)
	// Must not panic
	s.Start()
	s.Stop()
	s.Suffix(" Updated suffix")
}

func TestTTYSpinner_Suffix_UpdatesInnerSpinner(t *testing.T) {
	s := newSpinner(" Initial", true)
	s.Suffix(" Updated")
	if s.s.Suffix != " Updated" {
		t.Fatalf("expected suffix %q, got %q", " Updated", s.s.Suffix)
	}
}

func TestTTYSpinner_Suffix_NoopWhenNonTTY(t *testing.T) {
	s := newSpinner(" Initial", false)
	// Must not panic
	s.Suffix(" Updated")
}
