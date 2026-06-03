package buildinfo

import (
	"fmt"
	"runtime"
)

// Version, Commit, and Date are injected at build time via ldflags.
// They are the single source of truth for build information across the CLI.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// Set stores the build-time values injected via ldflags.
// Call this once at program startup before any other package reads these vars.
func Set(version, commit, date string) {
	Version = version
	Commit = commit
	Date = date
}

// UserAgent returns the User-Agent string for HTTP requests.
// Format: runware-cli/VERSION (commit/COMMIT; built DATE) Go/GOVERSION
func UserAgent() string {
	return fmt.Sprintf("runware-cli/%s (commit/%s; built %s) %s", Version, Commit, Date, runtime.Version())
}
