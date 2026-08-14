package serverless

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

// newSecretsCmd returns the "serverless secrets" command group for organisation
// secrets and their attachment to applications.
func newSecretsCmd(logger *log.Logger) *cobra.Command {
	cmd := stubGroup("secrets", "Manage organisation secrets for serverless applications")
	cmd.Long = "Manage organisation-scoped encrypted secrets, and attach them to serverless applications."
	cmd.AddCommand(
		newSecretsListCmd(logger),
		newSecretsSetCmd(logger),
		newSecretsRemoveCmd(logger),
		newSecretsAttachmentsCmd(logger),
		newSecretsAttachCmd(logger),
		newSecretsDetachCmd(logger),
	)
	return cmd
}

func newSecretsListCmd(logger *log.Logger) *cobra.Command {
	var (
		limit  int
		cursor string
	)

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List organisation secrets",
		Long: `List organisation secret metadata. Encrypted values are never returned.

To list secrets attached to an application, use 'secrets attachments'.`,
		Example: `  # list organisation secrets
  runware serverless secrets list

  # page through results
  runware serverless secrets list --limit 20 --cursor <nextCursor>`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateListLimit(limit); err != nil {
				return err
			}

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			return listOrgSecrets(cmd, client, limit, cursor)
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of secrets to return (1-100)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor from a previous nextCursor")
	return cmd
}

func listOrgSecrets(cmd *cobra.Command, client *serverlessapi.Client, limit int, cursor string) error {
	var params *serverlessapi.ListSecretsParams
	if limit > 0 || cursor != "" {
		params = &serverlessapi.ListSecretsParams{}
		params.Limit, params.Cursor = listPageParams(limit, cursor)
	}

	spin := cmdutil.NewSpinner("Fetching secrets...")
	spin.Start()

	page, err := client.ListSecrets(cmd.Context(), params)
	if err != nil {
		spin.Stop()
		return err
	}
	spin.Stop()

	return printPage(cmdutil.FormatFor(cmd), page, secretsResult(page.Data), cmd.ErrOrStderr(), "")
}

func newSecretsSetCmd(logger *log.Logger) *cobra.Command {
	var (
		value     string
		valueFile string
	)

	cmd := &cobra.Command{
		Use:   "set <name>",
		Short: "Create or update an organisation secret",
		Long: `Create an organisation-scoped secret, or update its value if the name already exists.

This does not attach the secret to an application. Use 'secrets attach' for that.
The secret value is never printed. Prefer --value-file so the value is not visible
in process lists; use --value-file - to read from stdin.`,
		Example: `  # create or update a secret from a file
  runware serverless secrets set FOO --value-file ./foo.txt

  # read the value from stdin
  printf '%s' "$FOO" | runware serverless secrets set FOO --value-file -

  # then attach it to an application
  runware serverless secrets attach my-app FOO`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			secretValue, err := readSecretValue(value, valueFile, cmd.InOrStdin())
			if err != nil {
				return err
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Saving secret %s...", name))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			sec, err := createOrUpdateSecret(cmd.Context(), client, name, secretValue)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return output.Print(cmdutil.FormatFor(cmd), secretResult(*sec))
		},
	}

	cmd.Flags().StringVar(&value, "value", "", "Secret value (visible in process lists; prefer --value-file)")
	cmd.Flags().StringVar(&valueFile, "value-file", "", "Read secret value from a file, or - for stdin")
	cmd.MarkFlagsMutuallyExclusive("value", "value-file")
	cmd.MarkFlagsOneRequired("value", "value-file")
	return cmd
}

func newSecretsRemoveCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove an organisation secret",
		Long: `Remove an organisation secret. Returns a conflict if any application still
has it attached — detach each holder with 'secrets detach' first.`,
		Example: `  # remove an organisation secret
  runware serverless secrets remove FOO`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			spin := cmdutil.NewSpinner(fmt.Sprintf("Removing secret %s...", name))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			if err := client.DeleteSecret(cmd.Context(), name); err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return output.Print(cmdutil.FormatFor(cmd), secretRemovedResult{Name: name})
		},
	}
	return cmd
}

func newSecretsAttachmentsCmd(logger *log.Logger) *cobra.Command {
	var (
		limit  int
		cursor string
	)

	cmd := &cobra.Command{
		Use:   "attachments <appId>",
		Short: "List secrets attached to an application",
		Long: `List secrets attached to an application, including any env-var name override.
Encrypted values are never returned.`,
		Example: `  # list secrets attached to an application
  runware serverless secrets attachments my-app

  # page through results
  runware serverless secrets attachments my-app --limit 20 --cursor <nextCursor>`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateListLimit(limit); err != nil {
				return err
			}
			app := args[0]

			var params *serverlessapi.ListDeploymentSecretsParams
			if limit > 0 || cursor != "" {
				params = &serverlessapi.ListDeploymentSecretsParams{}
				params.Limit, params.Cursor = listPageParams(limit, cursor)
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Fetching secrets attached to %s...", app))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			page, err := client.ListDeploymentSecrets(cmd.Context(), app, params)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return printPage(cmdutil.FormatFor(cmd), page, secretAttachmentsResult(page.Data), cmd.ErrOrStderr(), "")
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of secrets to return (1-100)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor from a previous nextCursor")
	return cmd
}

func newSecretsAttachCmd(logger *log.Logger) *cobra.Command {
	var envVarName string

	cmd := &cobra.Command{
		Use:   "attach <appId> <name>",
		Short: "Attach an organisation secret to an application",
		Long: `Record that an organisation secret is attached to an application, optionally
under a different environment variable name.

The organisation secret must already exist (see 'secrets set'). This is a
control-plane association only in this API release — it does not roll workers.`,
		Example: `  # attach a secret using its name as the env var
  runware serverless secrets attach my-app FOO

  # inject under a different env var name
  runware serverless secrets attach my-app FOO --env-var-name FOO_KEY`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := args[0]
			name := args[1]
			body := serverlessapi.SecretAttach{SecretName: name}
			if envVarName != "" {
				body.EnvVarName = &envVarName
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Attaching secret %s to %s...", name, app))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			if err := client.AttachDeploymentSecret(cmd.Context(), app, body); err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return output.Print(cmdutil.FormatFor(cmd), secretAttachResult{
				DeploymentID: app,
				Name:         name,
				EnvVarName:   envVarName,
			})
		},
	}

	cmd.Flags().StringVar(&envVarName, "env-var-name", "", "Environment variable name (omit to use the secret name)")
	return cmd
}

func newSecretsDetachCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "detach <appId> <name>",
		Short: "Detach a secret from an application",
		Long: `Remove the control-plane attachment from an application. Does not remove the
organisation secret.`,
		Example: `  # detach a secret from an application
  runware serverless secrets detach my-app FOO`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := args[0]
			name := args[1]

			spin := cmdutil.NewSpinner(fmt.Sprintf("Detaching secret %s from %s...", name, app))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			if err := client.DetachDeploymentSecret(cmd.Context(), app, name); err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return output.Print(cmdutil.FormatFor(cmd), secretDetachResult{
				DeploymentID: app,
				Name:         name,
			})
		},
	}
	return cmd
}

func readSecretValue(value, valueFile string, stdin io.Reader) (string, error) {
	if valueFile == "" {
		return value, nil
	}

	var (
		raw []byte
		err error
	)
	if valueFile == "-" {
		raw, err = io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("read secret from stdin: %w", err)
		}
	} else {
		raw, err = os.ReadFile(valueFile)
		if err != nil {
			return "", fmt.Errorf("read secret from %s: %w", valueFile, err)
		}
	}

	s := strings.TrimSuffix(string(raw), "\n")
	return strings.TrimSuffix(s, "\r"), nil
}

func createOrUpdateSecret(ctx context.Context, client *serverlessapi.Client, name, value string) (*serverlessapi.Secret, error) {
	sec, err := client.CreateSecret(ctx, serverlessapi.SecretCreate{
		Name:  name,
		Type:  serverlessapi.SecretTypeGeneric,
		Value: value,
	})
	if err == nil {
		return sec, nil
	}
	if !isHTTPConflict(err) {
		return nil, err
	}
	updated, updateErr := client.UpdateSecret(ctx, name, serverlessapi.SecretUpdate{Value: value})
	if isHTTPNotFound(updateErr) {
		// Name is reserved (e.g. pending_destroy) but not an updatable live secret.
		return nil, err
	}
	return updated, updateErr
}

func isHTTPConflict(err error) bool {
	var re *transport.RunwareError
	return errors.As(err, &re) && re.StatusCode == http.StatusConflict
}

func isHTTPNotFound(err error) bool {
	var re *transport.RunwareError
	return errors.As(err, &re) && re.StatusCode == http.StatusNotFound
}
