package model

import (
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

type modelSearchResults []api.ModelResult

func (r modelSearchResults) Headers() []string {
	return []string{"Name", "AIR", "Category", "Architecture", "Type", "Version"}
}

func (r modelSearchResults) Rows() [][]any {
	rows := make([][]any, len(r))
	for i := range r {
		rows[i] = []any{r[i].Name, r[i].AIR, r[i].Category, r[i].Architecture, r[i].Type, r[i].Version}
	}
	return rows
}

func newSearchCmd(logger *log.Logger) *cobra.Command {
	var flags struct {
		search       string
		category     string
		architecture string
		modelType    string
		visibility   string
		limit        int
		offset       int
	}

	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search models available on the Runware platform",
		Example: `  # Search for realistic image models
  runware model search --search "realistic"

  # Filter to SDXL checkpoints only
  runware model search --search "portrait" --category checkpoint --architecture sdxl

  # List your private models
  runware model search --search "my-model" --visibility private

  # Paginate through results
  runware model search --search "anime" --limit 10 --offset 20`,
		RunE: func(cmd *cobra.Command, args []string) error {
			t, err := cmdutil.NewTransport(cmd, slog.New(logger))
			if err != nil {
				return err
			}
			defer t.Close() //nolint:errcheck

			client := api.NewClient(t, slog.New(logger))

			if flags.limit < 1 || flags.limit > 100 {
				return fmt.Errorf("--limit must be between 1 and 100")
			}
			if flags.offset < 0 {
				return fmt.Errorf("--offset must be >= 0")
			}

			req := api.ModelSearchRequest{
				Search:       flags.search,
				Category:     flags.category,
				Architecture: flags.architecture,
				Type:         flags.modelType,
				Visibility:   flags.visibility,
				Limit:        flags.limit,
				Offset:       flags.offset,
			}

			result, err := client.ModelSearch(cmd.Context(), req)
			if err != nil {
				return err
			}

			if err := output.Print(cmdutil.FormatFor(cmd), modelSearchResults(result.Results)); err != nil {
				return err
			}

			showing := len(result.Results)
			fmt.Fprintf(os.Stderr, "Showing %d of %d results\n", showing, result.TotalResults)

			return nil
		},
	}

	f := cmd.Flags()
	f.StringVarP(&flags.search, "search", "q", "", "Search query (name, description, or AIR ID)")
	f.StringVarP(&flags.category, "category", "c", "", "Filter by category: "+strings.Join([]string{"checkpoint", "lora", "lycoris", "vae", "embeddings"}, ", "))
	f.StringVarP(&flags.architecture, "architecture", "a", "", "Filter by model architecture (e.g. sdxl, flux)")
	f.StringVarP(&flags.modelType, "type", "t", "", "Filter checkpoint type: base, inpainting, refiner")
	f.StringVar(&flags.visibility, "visibility", "public", "Filter by visibility: public, private, community, favorite")
	f.IntVarP(&flags.limit, "limit", "l", 20, "Maximum number of results to return (1-100)")
	f.IntVar(&flags.offset, "offset", 0, "Number of results to skip for pagination")

	if err := cmd.MarkFlagRequired("search"); err != nil {
		panic(err)
	}

	cmd.RegisterFlagCompletionFunc("category", func(_ *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) { //nolint:errcheck,gosec
		return []cobra.Completion{"checkpoint", "lora", "lycoris", "vae", "embeddings"}, cobra.ShellCompDirectiveNoFileComp
	})
	cmd.RegisterFlagCompletionFunc("type", func(_ *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) { //nolint:errcheck,gosec
		return []cobra.Completion{"base", "inpainting", "refiner"}, cobra.ShellCompDirectiveNoFileComp
	})
		return []cobra.Completion{"public", "private", "community", "favorite"}, cobra.ShellCompDirectiveNoFileComp
	})

	return cmd
}
