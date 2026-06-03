package auth

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/runware/runware-cli/internal/config"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newLoginCmd(logger *log.Logger) *cobra.Command {
	var key string

	cmd := &cobra.Command{
		Use:   "login",
		Short: "Authenticate with an API key",
		Example: `  # authenticate interactively
  runware auth login

  # pass API key directly
  runware auth login --key YOUR_API_KEY`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if key == "" {
				fmt.Fprint(os.Stderr, "Enter your Runware API key: ")
				if term.IsTerminal(int(os.Stdin.Fd())) {
					raw, err := term.ReadPassword(int(os.Stdin.Fd()))
					if err != nil {
						return fmt.Errorf("failed to read API key: %w", err)
					}
					fmt.Fprintln(os.Stderr)
					key = string(raw)
				} else {
					scanner := bufio.NewScanner(os.Stdin)
					if scanner.Scan() {
						key = scanner.Text()
					}
				}
			}

			key = strings.TrimSpace(key)
			if key == "" {
				return fmt.Errorf("API key cannot be empty")
			}

			t := transport.NewHTTPTransport(key, config.GetBaseURL(), slog.New(logger))
			client := api.NewClient(t, slog.New(logger))
			_, err := client.Ping(context.Background())
			if err != nil {
				return err
			}

			if err := config.SetAPIKey(key); err != nil {
				return fmt.Errorf("failed to save API key: %w", err)
			}

			logger.Info("✓ Authenticated successfully")
			return nil
		},
	}
	cmd.Flags().StringVarP(&key, "key", "k", "", "API key (or provide interactively)")
	return cmd
}
