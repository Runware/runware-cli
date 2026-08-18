package serverless

import (
	"log/slog"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

// gpuTypesResult wraps the GPU catalogue for display. JSON and YAML output the
// raw GpuType structs; the table renderer flattens them into columns.
type gpuTypesResult []serverlessapi.GpuType

func (r gpuTypesResult) Headers() []string {
	return []string{colID, colName, "Memory", "Availability", "Price ($/GPU/s)"}
}

func (r gpuTypesResult) Rows() [][]any {
	rows := make([][]any, len(r))
	for i := range r {
		g := &r[i]
		rows[i] = []any{
			g.Id,
			g.Name,
			g.Memory,
			string(g.Availability),
			g.Pricing.PerSecond,
		}
	}
	return rows
}

func newGPUsCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "gpus",
		Short: "List available GPU types and pricing",
		Example: `  # list GPU types and per-second pricing
  runware serverless gpus`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			spin := cmdutil.NewSpinner("Fetching GPU types...")
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			gpus, err := client.ListGpuTypes(cmd.Context())
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return output.Print(cmdutil.FormatFor(cmd), gpuTypesResult(gpus))
		},
	}
}
