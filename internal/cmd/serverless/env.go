package serverless

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

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
		envPairs  []string
		envFiles  []string
	)

	cmd := &cobra.Command{
		Use:   "set <appId> [key]",
		Short: "Create or update environment variables",
		Long: `Create or update plain-text environment variables.

A single key is the [key] argument with --value or --value-file. Prefer
--value-file so the value is not visible in process lists; use --value-file -
to read from stdin. A single-key write during an in-flight rollout returns
409 and does not store the change.

Several keys are repeatable --env KEY=VALUE, or --env-file. The command reads
the current set, merges these keys in, and writes the set once, so one rollout
carries all of them. Keys you do not mention stay when no other writer changes
the set between the read and the write. That write returns 409 while a create
or resume rollout is already in progress, and does not store the change.

A change records a new version with the same image and rolls the workload when
the app is active, initializing, or failed and its image is deployable. A
stopped or stopping app applies it on resume. An unchanged value records no
version.

The server rejects (HTTP 422) reserved platform names, names that collide
with an attached secret's injected env var, and adding a binding past the
100-variable-plus-secret ceiling. Overwriting an existing key is always
allowed.`,
		Example: `  # set one environment variable
  runware serverless apps env set my-app MY_KEY --value hello

  # read one value from a file
  runware serverless apps env set my-app MY_KEY --value-file ./value.txt

  # read one value from stdin
  printf '%s' "$MY_VALUE" | runware serverless apps env set my-app MY_KEY --value-file -

  # set several keys in one rollout
  runware serverless apps env set my-app --env FOO=bar --env BAZ=qux

  # set several keys from a file, in one rollout
  runware serverless apps env set my-app --env-file .env.deploy`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			appID := args[0]
			hasSingleValue := cmd.Flags().Changed("value") || cmd.Flags().Changed("value-file")
			bulk := len(envPairs) > 0 || len(envFiles) > 0
			if len(args) == 2 {
				if bulk {
					return fmt.Errorf("%s", envSetUsage)
				}
				return setOneEnvironmentVariable(cmd, logger, appID, args[1], value, valueFile)
			}
			if hasSingleValue {
				return fmt.Errorf("%s", envSetUsage)
			}
			updates, err := buildEnvironmentVariables(envFiles, envPairs)
			if err != nil {
				return err
			}
			if updates == nil || len(*updates) == 0 {
				return fmt.Errorf("%s", envSetUsage)
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Saving %d environment variables...", len(*updates)))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			if err := applyEnvUpdates(cmd.Context(), client, appID, *updates); err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return output.Print(cmdutil.FormatFor(cmd), envVarsResult(envVarsFromMap(*updates)))
		},
	}

	cmd.Flags().StringVar(&value, "value", "", "Variable value for a single <key> (visible in process lists; prefer --value-file)")
	cmd.Flags().StringVar(&valueFile, "value-file", "", "Read one <key> value from a file, or - for stdin")
	cmd.Flags().StringArrayVar(&envPairs, "env", nil, "Environment variable as KEY=VALUE, merged and written once (repeatable)")
	cmd.Flags().StringArrayVar(&envFiles, "env-file", nil, "File of KEY=VALUE lines to merge and write once (repeatable)")
	cmd.MarkFlagsMutuallyExclusive("value", "value-file")
	return cmd
}

const envSetUsage = "pass <key> with --value or --value-file, or use --env / --env-file"

func setOneEnvironmentVariable(cmd *cobra.Command, logger *log.Logger, appID, key, value, valueFile string) error {
	if !cmd.Flags().Changed("value") && !cmd.Flags().Changed("value-file") {
		return fmt.Errorf("%s", envSetUsage)
	}
	v, err := readValueFlag(value, valueFile, cmd.InOrStdin())
	if err != nil {
		return err
	}

	spin := cmdutil.NewSpinner(fmt.Sprintf("Saving environment variable %s...", key))
	spin.Start()

	client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
	ev, err := client.UpdateAppEnvironmentVariable(cmd.Context(), appID, key, serverlessapi.EnvironmentVariableUpdate{
		Value: v,
	})
	if err != nil {
		spin.Stop()
		return err
	}
	spin.Stop()

	return output.Print(cmdutil.FormatFor(cmd), envVarResult(*ev))
}

// applyEnvUpdates merges updates into the app's current variables and writes
// the whole set in one request. A key absent from updates is kept. The API
// treats the map as a replacement, so sending only the new keys would delete
// the rest.
func applyEnvUpdates(ctx context.Context, client *serverlessapi.Client, appID string, updates map[string]string) error {
	existing, err := listEnvironmentVariables(ctx, client, appID)
	if err != nil {
		return err
	}
	merged := envReplacement(existing, updates)
	_, err = client.UpdateApp(ctx, appID, serverlessapi.AppUpdate{
		EnvironmentVariables: &merged,
	})
	return err
}

func listEnvironmentVariables(ctx context.Context, client *serverlessapi.Client, appID string) (map[string]string, error) {
	out := map[string]string{}
	var cursor string
	for {
		params := &serverlessapi.ListAppEnvironmentVariablesParams{}
		params.Limit, params.Cursor = listPageParams(maxEnvVars, cursor)
		page, err := client.ListAppEnvironmentVariables(ctx, appID, params)
		if err != nil {
			return nil, err
		}
		for i := range page.Data {
			out[page.Data[i].Key] = page.Data[i].Value
		}
		if page.NextCursor == nil || *page.NextCursor == "" {
			return out, nil
		}
		cursor = *page.NextCursor
	}
}

func envReplacement(existing, updates map[string]string) map[string]*string {
	out := make(map[string]*string, len(existing)+len(updates))
	for key, value := range existing {
		val := value
		out[key] = &val
	}
	for key, value := range updates {
		val := value
		out[key] = &val
	}
	return out
}

func envVarsFromMap(updates map[string]string) []serverlessapi.EnvironmentVariable {
	keys := make([]string, 0, len(updates))
	for key := range updates {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	out := make([]serverlessapi.EnvironmentVariable, len(keys))
	for i, key := range keys {
		out[i] = serverlessapi.EnvironmentVariable{
			Key:   key,
			Value: updates[key],
		}
	}
	return out
}

func newAppsEnvUnsetCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "unset <appId> <key>",
		Short: "Remove an environment variable",
		Long: `Remove one plain-text environment variable from an application.

A delete records a new version with the same image and rolls the workload when
the app is active, initializing, or failed and its image is deployable. A
stopped or stopping app applies it on resume. A delete during an in-flight
rollout returns 409 and does not remove the value.`,
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
// --env / --env-file parsing, shared by deploy and apps env set.
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

// buildEnvironmentVariables turns --env KEY=VALUE pairs and --env-file paths
// into a name-to-value map. Files are read first, so an explicit --env wins
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
		// The trimmed copy decides whether the line carries an assignment at all;
		// the assignment itself is parsed from the original, because trailing
		// whitespace in a value can be deliberate and this is not the place to
		// silently rewrite it.
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		assignment := strings.TrimSuffix(line, "\r")
		// `export FOO=bar` is what a shell-sourced file looks like, and pasting
		// one in is the obvious mistake to absorb rather than reject. Trimmed on
		// the left only, so the value keeps whatever follows the `=`.
		assignment = strings.TrimPrefix(strings.TrimLeft(assignment, " \t"), "export ")
		name, value, err := splitEnvAssignment(assignment)
		if err != nil {
			return fmt.Errorf("%s line %d: %w", path, i+1, err)
		}
		env[name] = value
	}
	return nil
}

// unquote strips one matching pair of surrounding quotes.
//
// A .env file written by hand or produced by another tool routinely quotes
// values, and shells strip those quotes when sourcing the file. Passing them
// through would send `"hf_x"` as the token itself -- a 401 inside the pod with
// nothing in the logs to explain it. One pair only, and only when it matches, so
// a value that genuinely contains a quote keeps it.
func unquote(value string) string {
	if len(value) < 2 {
		return value
	}
	first, last := value[0], value[len(value)-1]
	if first != last {
		return value
	}
	if first == '\'' || first == '"' {
		return value[1 : len(value)-1]
	}
	return value
}

// splitEnvAssignment parses one KEY=VALUE, validating the name and value against
// the limits the server enforces.
func splitEnvAssignment(assignment string) (name, value string, err error) {
	name, value, found := strings.Cut(assignment, "=")
	if !found {
		return "", "", fmt.Errorf("%q is not KEY%s", assignment, envAssignSuffix)
	}
	// The name is trimmed; the value is not, beyond the quotes below. Trailing
	// whitespace in a value can be deliberate, and guessing costs more than it
	// saves -- a token with a stray character is a 401 the app cannot explain.
	name = strings.TrimSpace(name)
	value = unquote(value)

	switch {
	case name == "":
		return "", "", fmt.Errorf("%q has an empty name", assignment)
	// Counted in runes, not bytes: the API's maxLength is characters, so
	// len() would reject a valid non-ASCII value at half the documented limit.
	case utf8.RuneCountInString(name) > maxEnvNameLen:
		return "", "", fmt.Errorf("environment variable name %q exceeds %d characters", name, maxEnvNameLen)
	case !envNamePattern.MatchString(name):
		return "", "", fmt.Errorf(
			"environment variable name %q must be POSIX-style: letters, digits and underscore, not starting with a digit",
			name,
		)
	case utf8.RuneCountInString(value) > maxEnvValueLen:
		return "", "", fmt.Errorf("value for %q exceeds %d characters", name, maxEnvValueLen)
	}
	return name, value, nil
}
