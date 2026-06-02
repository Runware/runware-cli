package auth

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Manage authentication",
		Long:  "Login, logout, and check authentication status.",
	}
	cmd.AddCommand(newLoginCmd())
	cmd.AddCommand(newLogoutCmd())
	cmd.AddCommand(newStatusCmd())
	return cmd
}

func newLoginCmd() *cobra.Command {
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

			verbose, _ := cmd.Root().PersistentFlags().GetBool("verbose")
			client := api.NewClient(key, config.GetBaseURL(), verbose)
			_, err := client.Ping(context.Background())
			if err != nil {
				output.Error("Invalid API key. Authentication failed.")
				return err
			}

			if err := config.SetAPIKey(key); err != nil {
				return fmt.Errorf("failed to save API key: %w", err)
			}

			output.Success("Authenticated successfully")
			return nil
		},
	}
	cmd.Flags().StringVarP(&key, "key", "k", "", "API key (or provide interactively)")
	return cmd
}

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Clear stored credentials",
		Example: `  # clear stored credentials
  runware auth logout`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := config.RemoveAPIKey(); err != nil {
				return fmt.Errorf("failed to remove API key: %w", err)
			}
			output.Success("Logged out")
			return nil
		},
	}
}

func newStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show current auth state",
		Example: `  # show current auth state
  runware auth status`,
		RunE: func(cmd *cobra.Command, args []string) error {
			key := config.GetAPIKey()

			status := "not configured"
			maskedKey := "none"

			if key != "" {
				maskedKey = config.MaskKey(key)
				verbose, _ := cmd.Root().PersistentFlags().GetBool("verbose")
				client := api.NewClient(key, config.GetBaseURL(), verbose)
				_, err := client.Ping(context.Background())
				if err != nil {
					status = "invalid"
				} else {
					status = "valid"
				}
			}

			data := map[string]string{
				"api_key": maskedKey,
				"status":  status,
			}

			return output.Print(cmdutil.FormatFor(cmd), data, &output.Table{
				Headers: []string{"Field", "Value"},
				Rows:    [][]any{{"API Key", maskedKey}, {"Status", status}},
			})
		},
	}
}
