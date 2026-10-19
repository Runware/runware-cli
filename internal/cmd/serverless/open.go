package serverless

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/pkg/browser"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

// openResult reports the application and dashboard URL that was opened.
type openResult struct {
	AppID string `json:"appId" yaml:"appId"`
	URL   string `json:"url"   yaml:"url"`
}

func (r openResult) Headers() []string {
	return []string{colField, colValue}
}

func (r openResult) Rows() [][]any {
	return [][]any{
		{colApp, r.AppID},
		{"URL", r.URL},
	}
}

func newOpenCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "open <appId>",
		Short: "Open an application in the Runware dashboard",
		Example: `  # open an application's dashboard page in your browser
  runware serverless open my-model-abc

  # open the staging copy
  runware serverless open my-model-abc --env staging`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			dashboardURL := appDashboardURL(config.GetDashboardURL(), id, commandEnvironment(cmd))

			if err := browser.OpenURL(dashboardURL); err != nil {
				fmt.Fprintf(os.Stderr, "Could not open browser automatically: %v\n", err)
			}

			return output.Print(cmdutil.FormatFor(cmd), openResult{
				AppID: id,
				URL:   dashboardURL,
			})
		},
	}
	addEnvironmentFlag(cmd, cmd.Flags(), logger)
	return cmd
}

// appDashboardURL is the console page for the app's copy in environment.
func appDashboardURL(base, appID, environment string) string {
	link := strings.TrimSuffix(base, "/") + "/serverless/apps/" + url.PathEscape(appID)
	if environment == "" {
		return link
	}
	return link + "?" + url.Values{
		"environment": {environment},
	}.Encode()
}
