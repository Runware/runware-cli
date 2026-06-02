package output

import (
	"fmt"
	"os"
)

// Success prints a success message to stderr.
func Success(msg string) {
	fmt.Fprintf(os.Stderr, "✓ %s\n", msg)
}

// Error prints an error message to stderr.
func Error(msg string) {
	fmt.Fprintf(os.Stderr, "✗ %s\n", msg)
}

// Info prints an info message to stderr.
func Info(msg string) {
	fmt.Fprintf(os.Stderr, "• %s\n", msg)
}
