package serverless

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/pkg/browser"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

// openResult reports the application and dashboard URL that was opened.
type openResult struct {
	DeploymentID string `json:"deploymentId" yaml:"deploymentId"`
	URL          string `json:"url"          yaml:"url"`
}

func (r openResult) Headers() []string {
	return []string{colField, colValue}
}

func (r openResult) Rows() [][]any {
	return [][]any{
		{colApp, r.DeploymentID},
		{"URL", r.URL},
	}
}

func newOpenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "open <appId>",
		Short: "Open an application in the Runware dashboard",
		Example: `  # open an application's dashboard page in your browser
  runware serverless open my-model-abc`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			dashboardURL := strings.TrimSuffix(config.GetDashboardURL(), "/") + "/serverless/apps/" + url.PathEscape(id)

			if err := browser.OpenURL(dashboardURL); err != nil {
				fmt.Fprintf(os.Stderr, "Could not open browser automatically: %v\n", err)
			}

			return output.Print(cmdutil.FormatFor(cmd), openResult{
				DeploymentID: id,
				URL:          dashboardURL,
			})
		},
	}
}
