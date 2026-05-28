package output

import (
	"os"
	"time"

	"github.com/briandowns/spinner"
)

// TTYSpinner wraps spinner.Spinner with transparent TTY detection.
// All methods are no-ops when not running in a TTY.
type TTYSpinner struct {
	s *spinner.Spinner
}

// NewSpinner creates a TTYSpinner with the given suffix.
// The underlying spinner is only initialised when output is a TTY.
func NewSpinner(suffix string) *TTYSpinner {
	return newSpinner(suffix, IsTTY())
}

func newSpinner(suffix string, tty bool) *TTYSpinner {
	t := &TTYSpinner{}
	if tty {
		t.s = spinner.New(
			spinner.CharSets[14],
			100*time.Millisecond,
			spinner.WithWriter(os.Stderr),
			spinner.WithSuffix(suffix),
		)
	}
	return t
}

// Start starts the spinner. No-op when not a TTY.
func (t *TTYSpinner) Start() {
	if t.s != nil {
		t.s.Start()
	}
}

// Stop stops the spinner. No-op when not a TTY.
func (t *TTYSpinner) Stop() {
	if t.s != nil {
		t.s.Stop()
	}
}

// Suffix updates the spinner suffix text. No-op when not a TTY.
func (t *TTYSpinner) Suffix(suffix string) {
	if t.s != nil {
		t.s.Suffix = suffix
	}
}
