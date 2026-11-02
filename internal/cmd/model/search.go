package model

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

// modelSearchResults wraps results for table rendering, carrying the wide flag
// so Headers/Rows can vary the column set without extra types.
type modelSearchResults struct {
	models []api.ModelResult
	wide   bool
}

func (r modelSearchResults) Headers() []string {
	if r.wide {
		return []string{"Name", colAIR, colSource, "Category", "Architecture", colType, "Version", "Private", "Default Size", "Tags"}
	}
	return []string{"Name", colAIR, colSource, "Category", "Architecture", colType, "Version"}
}

func (r modelSearchResults) Rows() [][]any {
	rows := make([][]any, len(r.models))
	for i := range r.models {
		m := &r.models[i]
		if r.wide {
			rows[i] = []any{
				m.Name,
				m.AIR,
				m.Source,
				m.Category,
				m.Architecture,
				m.Type,
				m.Version,
				m.Private,
				formatDefaultSize(m.DefaultWidth, m.DefaultHeight),
				formatTags(m.Tags, 4),
			}
		} else {
			rows[i] = []any{m.Name, m.AIR, m.Source, m.Category, m.Architecture, m.Type, m.Version}
		}
	}
	return rows
}

// MarshalJSON delegates to the underlying slice so JSON output contains the raw model data.
func (r modelSearchResults) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.models)
}

// MarshalYAML delegates to the underlying slice so YAML output contains the raw model data.
func (r modelSearchResults) MarshalYAML() (any, error) {
	return r.models, nil
}

// formatDefaultSize renders a WxH string, or "—" when both are zero.
func formatDefaultSize(w, h int) string {
	if w == 0 && h == 0 {
		return "—"
	}
	return fmt.Sprintf("%d×%d", w, h)
}

// formatTags joins up to maxTags tags with ", " and appends "…" when there are more.
func formatTags(tags []string, maxTags int) string {
	if len(tags) == 0 {
		return "—"
	}
	shown := tags
	truncated := false
	if len(tags) > maxTags {
		shown = tags[:maxTags]
		truncated = true
	}
	s := strings.Join(shown, ", ")
	if truncated {
		s += "…"
	}
	return s
}

const (
	minSearchLength = 2
	maxSearchLength = 48
)

// validateSearch applies the API's rule for the search term: empty, or between
// minSearchLength and maxSearchLength characters.
func validateSearch(search string) error {
	n := utf8.RuneCountInString(search)
	if n != 0 && (n < minSearchLength || n > maxSearchLength) {
		return fmt.Errorf("--search must be between %d and %d characters", minSearchLength, maxSearchLength)
	}
	return nil
}

type searchFlags struct {
	search       string
	source       string
	category     string
	architecture string
	capabilities []string
	visibility   string
	limit        int
	offset       int
	sort         string
	wide         bool
}

func newSearchCmd(logger *log.Logger) *cobra.Command {
	var flags searchFlags

	cmd := &cobra.Command{
		Use:   "search",
		Short: "Search models available on the Runware platform",
		Example: `  # Search for realistic image models
  runware model search --search "realistic"

  # Filter to SDXL checkpoints only
  runware model search --search "portrait" --category checkpoint --architecture sdxl

  # Search only Runware's official models, sorted by name
  runware model search --search "flux" --source official --sort name

  # Filter by capability
  runware model search --source official --capabilities op:inpaint

  # Show extra columns including tags and default size
  runware model search --search "anime" --wide

  # List your private models
  runware model search --search "my-model" --visibility private

  # Paginate through results
  runware model search --search "anime" --limit 10 --offset 20`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateSearch(flags.search); err != nil {
				return err
			}
			if flags.limit < 1 || flags.limit > 100 {
				return fmt.Errorf("--limit must be between 1 and 100")
			}
			if flags.offset < 0 {
				return fmt.Errorf("--offset must be >= 0")
			}

			spin := cmdutil.NewSpinner("Searching models...")
			spin.Start()

			t, err := cmdutil.NewTransport(cmd, slog.New(logger))
			if err != nil {
				spin.Stop()
				return err
			}
			defer t.Close() //nolint:errcheck

			client := api.NewClient(t, slog.New(logger))

			req := api.ModelSearchRequest{
				Search:       flags.search,
				Source:       flags.source,
				Category:     flags.category,
				Architecture: flags.architecture,
				Capabilities: flags.capabilities,
				Visibility:   flags.visibility,
				Limit:        flags.limit,
				Offset:       flags.offset,
				Sort:         flags.sort,
			}

			result, err := client.ModelSearch(cmd.Context(), req)
			if err != nil {
				spin.Stop()
				return err
			}

			spin.Stop()
			if err := output.Print(cmdutil.FormatFor(cmd), modelSearchResults{
				models: result.Results,
				wide:   flags.wide,
			}); err != nil {
				return err
			}

			fmt.Fprintf(os.Stderr, "Showing %d of %d results\n", len(result.Results), result.TotalResults)

			return nil
		},
	}

	f := cmd.Flags()
	f.StringVarP(&flags.search, "search", "q", "", "Search query (name, description, or AIR ID), 2-48 characters")
	f.StringVar(&flags.source, "source", "", "Filter by model source: official, community (default: both)")
	f.StringVarP(&flags.category, "category", "c", "", "Filter by category: checkpoint, lora, lycoris, vae, embeddings")
	f.StringVarP(&flags.architecture, "architecture", "a", "", "Filter by model architecture (e.g. sdxl, flux)")
	f.StringSliceVar(&flags.capabilities, "capabilities", nil, "Filter by capabilities (repeatable: --capabilities io:text-to-image --capabilities op:inpaint)")
	f.StringVar(&flags.visibility, "visibility", "public", "Filter by visibility: public, private, favorite, owned")
	f.IntVarP(&flags.limit, "limit", "l", 20, "Maximum number of results to return (1-100)")
	f.IntVar(&flags.offset, "offset", 0, "Number of results to skip for pagination")
	f.StringVar(&flags.sort, "sort", "", "Sort order: _score, popularity, name, addedUnixTimestamp or updatedDateUnixTimestamp. A - prefix reverses any of them except _score (default: _score with --search, popularity without)")
	f.BoolVarP(&flags.wide, "wide", "W", false, "Show additional columns: private, default size, tags")

	cmd.RegisterFlagCompletionFunc("source", func(_ *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) { //nolint:errcheck,gosec
		return []cobra.Completion{"official", "community"}, cobra.ShellCompDirectiveNoFileComp
	})
	cmd.RegisterFlagCompletionFunc("category", func(_ *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) { //nolint:errcheck,gosec
		return []cobra.Completion{"checkpoint", "lora", "lycoris", "vae", "embeddings"}, cobra.ShellCompDirectiveNoFileComp
	})
	cmd.RegisterFlagCompletionFunc("visibility", func(_ *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) { //nolint:errcheck,gosec
		return []cobra.Completion{"public", "private", "favorite", "owned"}, cobra.ShellCompDirectiveNoFileComp
	})
	cmd.RegisterFlagCompletionFunc("sort", func(_ *cobra.Command, _ []string, _ string) ([]cobra.Completion, cobra.ShellCompDirective) { //nolint:errcheck,gosec
		return []cobra.Completion{
			"_score",
			"popularity",
			"-popularity",
			"name",
			"-name",
			"addedUnixTimestamp",
			"-addedUnixTimestamp",
			"updatedDateUnixTimestamp",
			"-updatedDateUnixTimestamp",
		}, cobra.ShellCompDirectiveNoFileComp
	})

	return cmd
}
