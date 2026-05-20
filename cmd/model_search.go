package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

var modelSearchCmd = &cobra.Command{
	Use:   "modelSearch [query]",
	Short: "Search available models",
	Long: `Search for models available on Runware.

Examples:
  runware modelSearch "flux"
  runware modelSearch "sdxl" --category checkpoint
  runware modelSearch --architecture flux1d --limit 10
  runware modelSearch "portrait" --category lora --format json`,
	Args: cobra.MaximumNArgs(1),
	RunE: runModelSearch,
}

func init() {
	f := modelSearchCmd.Flags()
	f.String("category", "", "Filter by category: checkpoint, lora, etc.")
	f.String("architecture", "", "Filter by architecture: flux1d, sdxl, sd15, etc.")
	f.Int("limit", 20, "Maximum number of results")
	f.Int("offset", 0, "Offset for pagination")

	modelSearchCmd.RegisterFlagCompletionFunc("category", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) { //nolint:errcheck,gosec
		return []string{"checkpoint", "lora", "controlnet", "vae", "embedding"}, cobra.ShellCompDirectiveNoFileComp
	})

	modelSearchCmd.RegisterFlagCompletionFunc("architecture", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) { //nolint:errcheck,gosec
		return []string{"flux1d", "flux1s", "sdxl", "sd15", "sd3"}, cobra.ShellCompDirectiveNoFileComp
	})
}

func runModelSearch(cmd *cobra.Command, args []string) error {
	key := config.GetAPIKey()
	if key == "" {
		output.Error("No API key configured. Run 'runware auth login' to authenticate.")
		return api.ErrNoAPIKey
	}

	req := &api.ModelSearchRequest{}

	if len(args) > 0 {
		req.Search = args[0]
	}

	if v, _ := cmd.Flags().GetString("category"); v != "" {
		req.Category = v
	}
	if v, _ := cmd.Flags().GetString("architecture"); v != "" {
		req.Architecture = v
	}
	if v, _ := cmd.Flags().GetInt("limit"); v > 0 {
		req.Limit = v
	}
	if v, _ := cmd.Flags().GetInt("offset"); v > 0 {
		req.Offset = v
	}

	client := api.NewClient(key, config.GetBaseURL(), flagVerbose)
	result, err := client.ModelSearch(context.Background(), req)
	if err != nil {
		if api.IsAuthError(err) {
			output.Error("Authentication failed. Run 'runware auth login' to set your API key.")
		}
		return err
	}

	format := output.ParseFormat(getFormat())

	if format != output.FormatTable {
		return output.Print(format, result, nil, nil)
	}

	if len(result.Results) == 0 {
		output.Info("No models found.")
		return nil
	}

	headers := []any{"AIR", "Name", "Category", "Arch", "Version"}
	var rows [][]any
	for i := range result.Results {
		m := &result.Results[i]
		name := truncate(m.Name, 45)
		rows = append(rows, []any{m.AIR, name, m.Category, m.Architecture, m.Version})
	}

	if err := output.Print(format, result, headers, rows); err != nil {
		return err
	}

	fmt.Fprintf(cmd.ErrOrStderr(), "\nShowing %d of %d results\n", len(result.Results), result.TotalResults) //nolint:errcheck,gosec
	return nil
}

func truncate(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
