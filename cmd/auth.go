package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/runware/runware-cli/internal/api"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Manage authentication",
	Long:  "Login, logout, and check authentication status.",
}

var authLoginKey string

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with an API key",
	RunE: func(cmd *cobra.Command, args []string) error {
		if authLoginKey == "" {
			fmt.Fprint(os.Stderr, "Enter your Runware API key: ")
			if term.IsTerminal(int(os.Stdin.Fd())) {
				raw, err := term.ReadPassword(int(os.Stdin.Fd()))
				if err != nil {
					return fmt.Errorf("failed to read API key: %w", err)
				}
				fmt.Fprintln(os.Stderr)
				authLoginKey = string(raw)
			} else {
				scanner := bufio.NewScanner(os.Stdin)
				if scanner.Scan() {
					authLoginKey = scanner.Text()
				}
			}
		}

		authLoginKey = strings.TrimSpace(authLoginKey)
		if authLoginKey == "" {
			return fmt.Errorf("API key cannot be empty")
		}

		// Validate key by pinging the API
		client := api.NewClient(authLoginKey, config.GetBaseURL(), flagVerbose)
		_, err := client.Ping(context.Background())
		if err != nil {
			output.Error("Invalid API key. Authentication failed.")
			return err
		}

		if err := config.SetAPIKey(authLoginKey); err != nil {
			return fmt.Errorf("failed to save API key: %w", err)
		}

		output.Success("Authenticated successfully")
		return nil
	},
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Clear stored credentials",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.RemoveAPIKey(); err != nil {
			return fmt.Errorf("failed to remove API key: %w", err)
		}
		output.Success("Logged out")
		return nil
	},
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current auth state",
	RunE: func(cmd *cobra.Command, args []string) error {
		key := config.GetAPIKey()

		status := "not configured"
		maskedKey := "none"

		if key != "" {
			maskedKey = config.MaskKey(key)
			// Verify the key
			client := api.NewClient(key, config.GetBaseURL(), flagVerbose)
			_, err := client.Ping(context.Background())
			if err != nil {
				status = "invalid"
			} else {
				status = "valid"
			}
		}

		format := output.ParseFormat(getFormat())
		data := map[string]string{
			"api_key": maskedKey,
			"status":  status,
		}

		return output.Print(format, data,
			[]any{"Field", "Value"},
			[][]any{
				{"API Key", maskedKey},
				{"Status", status},
			},
		)
	},
}

func init() {
	authLoginCmd.Flags().StringVarP(&authLoginKey, "key", "k", "", "API key (or provide interactively)")
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authLogoutCmd)
	authCmd.AddCommand(authStatusCmd)
}
