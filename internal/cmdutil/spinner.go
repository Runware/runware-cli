package cmdutil

import (
	"os"
	"time"

	"github.com/briandowns/spinner"
)

// Spinner wraps the briandowns/spinner with a simplified API for consistent
// processing-state feedback across all CLI commands.
type Spinner struct {
	s *spinner.Spinner
}

// NewSpinner creates a spinner that writes to stderr with the given initial message.
func NewSpinner(message string) *Spinner {
	s := spinner.New(spinner.CharSets[14], 100*time.Millisecond,
		spinner.WithWriter(os.Stderr))
	s.Suffix = " " + message
	return &Spinner{s: s}
}

// Start begins the spinner animation.
func (sp *Spinner) Start() { sp.s.Start() }

// Stop halts the spinner animation and clears the line.
func (sp *Spinner) Stop() { sp.s.Stop() }

// SetMessage updates the spinner suffix text while it is running.
func (sp *Spinner) SetMessage(msg string) { sp.s.Suffix = " " + msg }

// Restart stops and restarts the spinner. Use this instead of Stop+Start
// to reliably resume the animation.
func (sp *Spinner) Restart() { sp.s.Restart() }
