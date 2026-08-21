package serverless

import (
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

func newAppsEnvCmd(logger *log.Logger) *cobra.Command {
	cmd := stubGroup("env", "Manage plain-text environment variables for an application")
	cmd.Long = `Manage plain-text environment variables on a serverless application.

These are not organisation secrets. Values are returned by list and set.
Use 'serverless secrets' for encrypted secrets attached as env vars.`
	cmd.AddCommand(
		newAppsEnvListCmd(logger),
		newAppsEnvSetCmd(logger),
		newAppsEnvUnsetCmd(logger),
	)
	return cmd
}

func newAppsEnvListCmd(logger *log.Logger) *cobra.Command {
	var (
		limit  int
		cursor string
	)

	cmd := &cobra.Command{
		Use:   "list <appId>",
		Short: "List environment variables for a serverless application",
		Long: `List plain-text environment variables for an application, including values.

To list encrypted secrets attached to an application, use 'serverless secrets attachments'.`,
		Example: `  # list environment variables
  runware serverless apps env list my-app

  # page through results
  runware serverless apps env list my-app --limit 20 --cursor <nextCursor>`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateListLimit(limit); err != nil {
				return err
			}
			id := args[0]
			var params *serverlessapi.ListAppEnvironmentVariablesParams
			if limit > 0 || cursor != "" {
				params = &serverlessapi.ListAppEnvironmentVariablesParams{}
				params.Limit, params.Cursor = listPageParams(limit, cursor)
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Fetching environment variables for %s...", id))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			page, err := client.ListAppEnvironmentVariables(cmd.Context(), id, params)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return printPage(cmdutil.FormatFor(cmd), page, envVarsResult(page.Data), cmd.ErrOrStderr(), "")
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of environment variables to return (1-100)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor from a previous nextCursor")
	return cmd
}

func newAppsEnvSetCmd(logger *log.Logger) *cobra.Command {
	var (
		value     string
		valueFile string
	)

	cmd := &cobra.Command{
		Use:   "set <appId> <key>",
		Short: "Create or update an environment variable",
		Long: `Create or update one plain-text environment variable.

Prefer --value-file so the value is not visible in process lists; use
--value-file - to read from stdin.

The server rejects (HTTP 422) reserved platform names, names that collide
with an attached secret's injected env var, and adding a binding past the
100-variable-plus-secret ceiling. Overwriting an existing key is always
allowed.`,
		Example: `  # set an environment variable
  runware serverless apps env set my-app MY_KEY --value hello

  # read the value from a file
  runware serverless apps env set my-app MY_KEY --value-file ./value.txt

  # read the value from stdin
  printf '%s' "$MY_VALUE" | runware serverless apps env set my-app MY_KEY --value-file -`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := args[0]
			key := args[1]
			v, err := readValueFlag(value, valueFile, cmd.InOrStdin())
			if err != nil {
				return err
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Saving environment variable %s...", key))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			ev, err := client.UpdateAppEnvironmentVariable(cmd.Context(), app, key, serverlessapi.EnvironmentVariableUpdate{
				Value: v,
			})
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return output.Print(cmdutil.FormatFor(cmd), envVarResult(*ev))
		},
	}

	cmd.Flags().StringVar(&value, "value", "", "Variable value (visible in process lists; prefer --value-file)")
	cmd.Flags().StringVar(&valueFile, "value-file", "", "Read variable value from a file, or - for stdin")
	cmd.MarkFlagsMutuallyExclusive("value", "value-file")
	cmd.MarkFlagsOneRequired("value", "value-file")
	return cmd
}

func newAppsEnvUnsetCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "unset <appId> <key>",
		Short: "Remove an environment variable",
		Long:  "Remove one plain-text environment variable from an application.",
		Example: `  # remove an environment variable
  runware serverless apps env unset my-app MY_KEY`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := args[0]
			key := args[1]

			spin := cmdutil.NewSpinner(fmt.Sprintf("Removing environment variable %s...", key))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			if err := client.DeleteAppEnvironmentVariable(cmd.Context(), app, key); err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return output.Print(cmdutil.FormatFor(cmd), envUnsetResult{
				AppID: app,
				Key:   key,
			})
		},
	}
}

// ---------------------------------------------------------------------------
// Create-time environment variables, for `deploy --env` / `--env-file`.
// ---------------------------------------------------------------------------

// Environment variable limits, mirrored from the server's EnvironmentVariableName
// and its deployment_configs column CHECK.
const (
	maxEnvVars      = 100
	maxEnvNameLen   = 128
	maxEnvValueLen  = 4096
	envAssignSuffix = "=VALUE"
)

// envNamePattern is the server's EnvironmentVariableName rule: POSIX-style, so a
// name this accepts is a name the API can store rather than one it rejects after
// the archive has already been uploaded.
var envNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,127}$`)

// buildEnvironmentVariables turns --env KEY=VALUE pairs and --env-file paths into
// the create request's map.
//
// These belong on the CREATE request and nowhere else: an app's environment is
// frozen into its version snapshot, which is what the deployer renders from, and
// no endpoint creates a further version -- `deploy` re-applies an existing one by
// number and says so. So a variable set through the /environment-variables
// endpoints after the app exists is stored, listed back, and never reaches a
// worker. Passing it here is the only route that ends up in a pod.
//
// Files are read before the inline pairs are applied, so an explicit --env wins
// over a file entry with the same name.
func buildEnvironmentVariables(files, pairs []string) (*map[string]string, error) {
	if len(files) == 0 && len(pairs) == 0 {
		return nil, nil
	}

	env := make(map[string]string)
	for _, path := range files {
		if err := readEnvFile(path, env); err != nil {
			return nil, err
		}
	}
	for _, pair := range pairs {
		name, value, err := splitEnvAssignment(pair)
		if err != nil {
			return nil, err
		}
		env[name] = value
	}

	if len(env) > maxEnvVars {
		return nil, fmt.Errorf("at most %d environment variables (got %d)", maxEnvVars, len(env))
	}
	return &env, nil
}

// readEnvFile reads KEY=VALUE lines into env, skipping blanks and # comments.
func readEnvFile(path string, env map[string]string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read env file: %w", err)
	}
	for i, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		// `export FOO=bar` is what a shell-sourced file looks like, and pasting one
		// in is the obvious mistake to absorb rather than reject.
		trimmed = strings.TrimPrefix(trimmed, "export ")
		name, value, err := splitEnvAssignment(trimmed)
		if err != nil {
			return fmt.Errorf("%s line %d: %w", path, i+1, err)
		}
		env[name] = value
	}
	return nil
}

// splitEnvAssignment parses one KEY=VALUE, validating the name and value against
// the limits the server enforces.
func splitEnvAssignment(assignment string) (name, value string, err error) {
	name, value, found := strings.Cut(assignment, "=")
	if !found {
		return "", "", fmt.Errorf("%q is not KEY%s", assignment, envAssignSuffix)
	}
	// The name is trimmed but the value is not: trailing whitespace in a value can
	// be deliberate, and a token with a stray newline is a 401 the app cannot
	// explain -- so callers pass values through a file rather than have this guess.
	name = strings.TrimSpace(name)

	switch {
	case name == "":
		return "", "", fmt.Errorf("%q has an empty name", assignment)
	case len(name) > maxEnvNameLen:
		return "", "", fmt.Errorf("environment variable name %q exceeds %d characters", name, maxEnvNameLen)
	case !envNamePattern.MatchString(name):
		return "", "", fmt.Errorf(
			"environment variable name %q must be POSIX-style: letters, digits and underscore, not starting with a digit",
			name,
		)
	case len(value) > maxEnvValueLen:
		return "", "", fmt.Errorf("value for %q exceeds %d characters", name, maxEnvValueLen)
	}
	return name, value, nil
}
