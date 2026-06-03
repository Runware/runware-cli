package version

import (
	"runtime"

	"github.com/runware/runware-cli/internal/buildinfo"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

type versionResult struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	Go      string `json:"go"`
}

func (r versionResult) Headers() []string {
	return []string{"Field", "Value"}
}

func (r versionResult) Rows() [][]any {
	return [][]any{
		{"version", r.Version},
		{"commit", r.Commit},
		{"built", r.Date},
		{"go", r.Go},
	}
}

// NewCmd returns the version command for printing build information.
func NewCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Example: `  # print version information
  runware version`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return output.Print(cmdutil.FormatFor(cmd), versionResult{
				Version: buildinfo.Version,
				Commit:  buildinfo.Commit,
				Date:    buildinfo.Date,
				Go:      runtime.Version(),
			})
		},
	}
}
