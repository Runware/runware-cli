package cmd

import (
	"bufio"
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
	Long:  "Login, logout, check status, and switch environments.",
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Authenticate with an API key",
	RunE: func(cmd *cobra.Command, args []string) error {
		key, _ := cmd.Flags().GetString("key")

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

		// Validate key by pinging the API
		ctx, cancel := contextWithTimeout(cmd)
		defer cancel()

		client := api.NewClient(key, config.GetBaseURL(), flagVerbose)
		_, err := client.Ping(ctx)
		if err != nil {
			output.Error("Invalid API key. Authentication failed.")
			return err
		}

		env := config.GetEnvironment()
		if err := config.SetAPIKey(env, key); err != nil {
			return fmt.Errorf("failed to save API key: %w", err)
		}

		output.Success(fmt.Sprintf("Authenticated successfully (%s) — key: %s", env, config.MaskKey(key)))
		return nil
	},
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Clear stored credentials",
	RunE: func(cmd *cobra.Command, args []string) error {
		env := config.GetEnvironment()
		if err := config.RemoveAPIKey(env); err != nil {
			return fmt.Errorf("failed to remove API key: %w", err)
		}
		output.Success(fmt.Sprintf("Logged out from %s", env))
		return nil
	},
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show current auth state and environment",
	RunE: func(cmd *cobra.Command, args []string) error {
		env := config.GetEnvironment()
		key := config.GetAPIKey()
		mode := config.GetMode()

		status := "not configured"
		maskedKey := "none"

		if key != "" {
			maskedKey = config.MaskKey(key)
			// Verify the key
			ctx, cancel := contextWithTimeout(cmd)
			defer cancel()

			client := api.NewClient(key, config.GetBaseURL(), flagVerbose)
			_, err := client.Ping(ctx)
			if err != nil {
				status = "invalid"
			} else {
				status = "valid"
			}
		}

		format := output.ParseFormat(getFormat())
		data := map[string]string{
			"environment": env,
			"api_key":     maskedKey,
			"status":      status,
			"mode":        mode,
		}

		return output.Print(format, data,
			[]any{"Field", "Value"},
			[][]any{
				{"Environment", env},
				{"API Key", maskedKey},
				{"Status", status},
				{"Mode", mode},
			},
		)
	},
}

var authSwitchCmd = &cobra.Command{
	Use:   "switch [production|staging]",
	Short: "Switch between environments",
	Args:  cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return []string{"production", "staging"}, cobra.ShellCompDirectiveNoFileComp
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		env := strings.ToLower(args[0])
		if env != "production" && env != "staging" {
			return fmt.Errorf("environment must be 'production' or 'staging'")
		}

		if err := config.SetEnvironment(env); err != nil {
			return fmt.Errorf("failed to switch environment: %w", err)
		}

		output.Success(fmt.Sprintf("Switched to %s", env))
		return nil
	},
}

func init() {
	authLoginCmd.Flags().String("key", "", "API key (or provide interactively)")
	authCmd.AddCommand(authLoginCmd)
	authCmd.AddCommand(authLogoutCmd)
	authCmd.AddCommand(authStatusCmd)
	authCmd.AddCommand(authSwitchCmd)
}
