package cmdutil

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/runware/runware-cli/internal/config"
	"github.com/spf13/cobra"
)

// NewTransport dials a transport using the --transport flag on the root command
// (whose default reflects the config's defaults.transport) and the API key from
// config. Callers are responsible for calling Close on the returned transport.
func NewTransport(cmd *cobra.Command, logger *slog.Logger) (transport.Transport, error) {
	tp, _ := cmd.Root().PersistentFlags().GetString("transport")
	tp = strings.ToLower(tp)
	var url string
	switch tp {
	case transport.SchemeHTTP:
		url = config.GetBaseURL()
	case transport.SchemeWS:
		url = config.GetWSBaseURL()
	default:
		return nil, fmt.Errorf("unknown transport %q: must be one of: %s", tp, strings.Join(transport.ValidTransports(), ", "))
	}
	return transport.DialContext(cmd.Context(), tp, config.GetAPIKey(), url, logger)
}
