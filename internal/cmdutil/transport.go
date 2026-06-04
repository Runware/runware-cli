package cmdutil

import (
	"fmt"
	"log/slog"

	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/runware/runware-cli/internal/config"
	"github.com/spf13/cobra"
)

// NewTransport dials a transport using the --transport flag on the root command
// and the API key from config. Callers are responsible for calling Close on the
// returned transport.
func NewTransport(cmd *cobra.Command, logger *slog.Logger) (transport.Transport, error) {
	tp, _ := cmd.Root().PersistentFlags().GetString("transport")
	var url string
	switch tp {
	case "http":
		url = config.GetBaseURL()
	case "ws":
		url = config.GetWSBaseURL()
	default:
		return nil, fmt.Errorf("unknown transport %q: must be \"ws\" or \"http\"", tp)
	}
	return transport.DialContext(cmd.Context(), tp, config.GetAPIKey(), url, logger)
}
