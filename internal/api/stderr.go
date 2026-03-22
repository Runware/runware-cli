package api

import (
	"io"
	"os"
)

// getStderr returns stderr for verbose logging.
func getStderr() io.Writer {
	return os.Stderr
}
