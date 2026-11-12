package serverless

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

const (
	connectorsListUse            = "list"
	appConnectorsListUse         = "list <appId>"
	flagKind                     = "kind"
	flagAudience                 = "audience"
	flagRoleARN                  = "role-arn"
	flagWorkloadIdentityProvider = "workload-identity-provider"
	flagServiceAccountEmail      = "service-account-email"
)

// connectorKinds are the --kind values, as the API names them.
var connectorKinds = []serverlessapi.OidcConnectorKind{
	serverlessapi.OidcConnectorKindAws,
	serverlessapi.OidcConnectorKindGcp,
	serverlessapi.OidcConnectorKindGeneric,
}

// gcpAudiencePrefix starts the audience of every gcp connector, which is
// always this followed by the workload identity provider.
const gcpAudiencePrefix = "//iam.googleapis.com/"

// connectorFlags are the provider flags of connectors create and update.
type connectorFlags struct {
	audience                 string
	roleARN                  string
	workloadIdentityProvider string
	serviceAccountEmail      string
}

func (f *connectorFlags) add(cmd *cobra.Command) {
	cmd.Flags().StringVar(&f.audience, flagAudience, "", "Token audience (aud). Required for generic; aws defaults to runware:<organization uuid>; gcp derives it from the provider and refuses it")
	cmd.Flags().StringVar(&f.roleARN, flagRoleARN, "", "aws: IAM role the worker assumes")
	cmd.Flags().StringVar(&f.workloadIdentityProvider, flagWorkloadIdentityProvider, "", "gcp: Workload Identity Federation provider resource name")
	cmd.Flags().StringVar(&f.serviceAccountEmail, flagServiceAccountEmail, "", "gcp: service account to impersonate (optional)")
}

// foreignFlags returns the provider flags that kind does not take and that
// were set.
func (f *connectorFlags) foreignFlags(kind serverlessapi.OidcConnectorKind) []string {
	var foreign []string
	if kind != serverlessapi.OidcConnectorKindAws && f.roleARN != "" {
		foreign = append(foreign, "--"+flagRoleARN)
	}
	if kind == serverlessapi.OidcConnectorKindGcp && f.audience != "" {
		// A gcp audience is always derived from the provider; the API refuses any other.
		foreign = append(foreign, "--"+flagAudience)
	}
	if kind != serverlessapi.OidcConnectorKindGcp {
		if f.workloadIdentityProvider != "" {
			foreign = append(foreign, "--"+flagWorkloadIdentityProvider)
		}
		if f.serviceAccountEmail != "" {
			foreign = append(foreign, "--"+flagServiceAccountEmail)
		}
	}
	return foreign
}

// checkNotEmpty refuses a provider flag that was given an empty value: it would
// read as not given, and an update would roll every live deployment for nothing.
func (f *connectorFlags) checkNotEmpty(cmd *cobra.Command) error {
	for _, name := range []string{flagAudience, flagRoleARN, flagWorkloadIdentityProvider, flagServiceAccountEmail} {
		if cmd.Flags().Changed(name) && cmd.Flags().Lookup(name).Value.String() == "" {
			return errors.New("--" + name + " must not be empty")
		}
	}
	return nil
}

func (f *connectorFlags) checkKind(kind serverlessapi.OidcConnectorKind) error {
	if foreign := f.foreignFlags(kind); len(foreign) > 0 {
		return fmt.Errorf("%s do not apply to a %s connector", strings.Join(foreign, ", "), kind)
	}
	return nil
}

// parseConnectorKind validates a --kind value.
func parseConnectorKind(s string) (serverlessapi.OidcConnectorKind, error) {
	for _, k := range connectorKinds {
		if string(k) == s {
			return k, nil
		}
	}
	names := make([]string, len(connectorKinds))
	for i, k := range connectorKinds {
		names[i] = string(k)
	}
	return "", fmt.Errorf("invalid --kind %q: must be one of: %s", s, strings.Join(names, ", "))
}

// createBody builds the createOidcConnector body for kind, refusing a kind
// that lacks the flag it needs before any request is sent.
func (f *connectorFlags) createBody(name string, kind serverlessapi.OidcConnectorKind) (serverlessapi.OidcConnectorCreate, error) {
	if err := f.checkKind(kind); err != nil {
		return serverlessapi.OidcConnectorCreate{}, err
	}
	body := serverlessapi.OidcConnectorCreate{
		Name: name,
		Kind: kind,
	}
	if f.audience != "" {
		body.Audience = &f.audience
	}
	switch kind {
	case serverlessapi.OidcConnectorKindAws:
		if f.roleARN == "" {
			return body, errors.New("--" + flagRoleARN + " is required for an aws connector")
		}
		body.Aws = &serverlessapi.OidcConnectorAws{RoleArn: f.roleARN}
	case serverlessapi.OidcConnectorKindGcp:
		if f.workloadIdentityProvider == "" {
			return body, errors.New("--" + flagWorkloadIdentityProvider + " is required for a gcp connector")
		}
		body.Gcp = &serverlessapi.OidcConnectorGcp{WorkloadIdentityProvider: f.workloadIdentityProvider}
		if f.serviceAccountEmail != "" {
			body.Gcp.ServiceAccountEmail = &f.serviceAccountEmail
		}
	case serverlessapi.OidcConnectorKindGeneric:
		if f.audience == "" {
			return body, errors.New("--" + flagAudience + " is required for a generic connector")
		}
	}
	return body, nil
}

// updateBody builds the updateOidcConnector body. The API replaces the whole
// configuration, so what the flags leave out is carried over from current.
func (f *connectorFlags) updateBody(current *serverlessapi.OidcConnector) (serverlessapi.OidcConnectorUpdate, error) {
	if err := f.checkKind(current.Kind); err != nil {
		return serverlessapi.OidcConnectorUpdate{}, err
	}
	body := serverlessapi.OidcConnectorUpdate{Audience: &current.Audience}
	if f.audience != "" {
		body.Audience = &f.audience
	}
	switch current.Kind {
	case serverlessapi.OidcConnectorKindAws:
		aws := serverlessapi.OidcConnectorAws{}
		if current.Aws != nil {
			aws = *current.Aws
		}
		if f.roleARN != "" {
			aws.RoleArn = f.roleARN
		}
		body.Aws = &aws
	case serverlessapi.OidcConnectorKindGcp:
		gcp := serverlessapi.OidcConnectorGcp{}
		if current.Gcp != nil {
			gcp = *current.Gcp
		}
		if f.workloadIdentityProvider != "" {
			gcp.WorkloadIdentityProvider = f.workloadIdentityProvider
		}
		if f.serviceAccountEmail != "" {
			gcp.ServiceAccountEmail = &f.serviceAccountEmail
		}
		body.Gcp = &gcp
		// The audience follows the provider, so a changed provider needs a new one.
		audience := gcpAudiencePrefix + gcp.WorkloadIdentityProvider
		body.Audience = &audience
	case serverlessapi.OidcConnectorKindGeneric:
	}
	return body, nil
}

// newConnectorsCmd returns the "serverless connectors" command group for the
// organization's OIDC connectors.
func newConnectorsCmd(logger *log.Logger) *cobra.Command {
	cmd := stubGroup("connectors", "Manage OIDC connectors")
	cmd.Long = `Manage OIDC connectors: the external providers (AWS, GCP or any OIDC verifier)
that an application's workers authenticate to with a short-lived token the
platform signs, instead of a stored credential. A connector holds no secret.

Connectors belong to the organization, not to an environment. Attach one to an
application with 'apps connectors attach'.`
	cmd.AddCommand(
		newConnectorsListCmd(logger),
		newConnectorsShowCmd(logger),
		newConnectorsCreateCmd(logger),
		newConnectorsUpdateCmd(logger),
		newConnectorsDeleteCmd(logger),
	)
	return cmd
}

func newConnectorsListCmd(logger *log.Logger) *cobra.Command {
	var (
		limit  int
		cursor string
	)

	cmd := &cobra.Command{
		Use:   connectorsListUse,
		Short: "List OIDC connectors",
		Long:  `List the organization's OIDC connectors.`,
		Example: `  runware serverless connectors list

  # page through results
  runware serverless connectors list --limit 20 --cursor <nextCursor>`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateListLimit(limit); err != nil {
				return err
			}
			var params serverlessapi.ListOidcConnectorsParams
			params.Limit, params.Cursor = listPageParams(limit, cursor)

			spin := cmdutil.NewSpinner("Fetching connectors...")
			spin.Start()
			page, err := newServerlessClient(cmd, logger).ListOidcConnectors(cmd.Context(), params)
			spin.Stop()
			if err != nil {
				return err
			}

			format := cmdutil.FormatFor(cmd)
			if format == output.FormatYAML {
				return output.Print(format, camelKeys{V: page})
			}
			return printPage(format, page, oidcConnectorsResult(page.Data), cmd.ErrOrStderr(), "")
		},
	}

	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum number of connectors to return (1-100)")
	cmd.Flags().StringVar(&cursor, "cursor", "", "Pagination cursor from a previous nextCursor")
	return cmd
}

func newConnectorsShowCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:     "show <name>",
		Short:   "Show an OIDC connector",
		Long:    `Show one OIDC connector, including the issuer to register with your cloud provider.`,
		Example: `  runware serverless connectors show aws-prod`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			spin := cmdutil.NewSpinner(fmt.Sprintf("Fetching connector %s...", name))
			spin.Start()
			connector, err := newServerlessClient(cmd, logger).GetOidcConnector(cmd.Context(), name)
			spin.Stop()
			if err != nil {
				return err
			}

			return output.Print(cmdutil.FormatFor(cmd), camelKeysTable{oidcConnectorResult(*connector)})
		},
	}
}

func newConnectorsCreateCmd(logger *log.Logger) *cobra.Command {
	var (
		kind  string
		flags connectorFlags
	)

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create an OIDC connector",
		Long: `Create an OIDC connector. --kind is fixed once created.

  aws      needs --role-arn. The worker gets AWS_ROLE_ARN and
           AWS_WEB_IDENTITY_TOKEN_FILE, which the AWS SDKs read unchanged.
  gcp      needs --workload-identity-provider, and takes an optional
           --service-account-email to impersonate. The worker gets
           GOOGLE_APPLICATION_CREDENTIALS, which google-auth reads unchanged.
  generic  needs --audience. The worker gets the token file and
           RUNWARE_OIDC_TOKEN_FILE.

Creating a connector does not give any application a token. Attach it with
'apps connectors attach'; that prints the subject a trust rule should match.`,
		Example: `  # AWS
  runware serverless connectors create aws-prod --kind aws \
    --role-arn arn:aws:iam::123456789012:role/runware-worker

  # GCP
  runware serverless connectors create gcp-prod --kind gcp \
    --workload-identity-provider projects/123456789/locations/global/workloadIdentityPools/runware/providers/runware \
    --service-account-email worker@my-project.iam.gserviceaccount.com

  # any other OIDC verifier
  runware serverless connectors create vault --kind generic --audience https://vault.example.com`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := flags.checkNotEmpty(cmd); err != nil {
				return err
			}
			parsed, err := parseConnectorKind(kind)
			if err != nil {
				return err
			}
			body, err := flags.createBody(name, parsed)
			if err != nil {
				return err
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Creating connector %s...", name))
			spin.Start()
			connector, err := newServerlessClient(cmd, logger).CreateOidcConnector(cmd.Context(), body)
			spin.Stop()
			if err != nil {
				return err
			}

			return output.Print(cmdutil.FormatFor(cmd), camelKeysTable{oidcConnectorResult(*connector)})
		},
	}

	cmd.Flags().StringVar(&kind, flagKind, "", "Provider: aws, gcp or generic")
	if err := cmd.MarkFlagRequired(flagKind); err != nil {
		panic(err)
	}
	if err := cmd.RegisterFlagCompletionFunc(flagKind, cobra.FixedCompletions([]cobra.Completion{"aws", "gcp", "generic"}, cobra.ShellCompDirectiveNoFileComp)); err != nil {
		panic(err)
	}
	flags.add(cmd)
	return cmd
}

func newConnectorsUpdateCmd(logger *log.Logger) *cobra.Command {
	var flags connectorFlags

	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update an OIDC connector",
		Long: `Change a connector's audience or provider fields. Fields you do not name keep
their current value. The name and kind cannot change.

Updating rolls every live application that attaches the connector, so its
workers get the new configuration. Running workers keep the old configuration
until they roll.`,
		Example: `  runware serverless connectors update aws-prod \
    --role-arn arn:aws:iam::123456789012:role/runware-worker-v2`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := flags.checkNotEmpty(cmd); err != nil {
				return err
			}
			client := newServerlessClient(cmd, logger)

			spin := cmdutil.NewSpinner(fmt.Sprintf("Updating connector %s...", name))
			spin.Start()
			defer spin.Stop()

			current, err := client.GetOidcConnector(cmd.Context(), name)
			if err != nil {
				return err
			}
			body, err := flags.updateBody(current)
			if err != nil {
				return err
			}
			connector, err := client.UpdateOidcConnector(cmd.Context(), name, body)
			if err != nil {
				return err
			}
			spin.Stop()

			return output.Print(cmdutil.FormatFor(cmd), camelKeysTable{oidcConnectorResult(*connector)})
		},
	}

	flags.add(cmd)
	cmd.MarkFlagsOneRequired(flagAudience, flagRoleARN, flagWorkloadIdentityProvider, flagServiceAccountEmail)
	return cmd
}

func newConnectorsDeleteCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete an OIDC connector",
		Long: `Delete an OIDC connector. It is refused with a conflict while any application
still attaches it: detach it from each one first with 'apps connectors detach'.`,
		Example: `  runware serverless connectors delete aws-prod`,
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			spin := cmdutil.NewSpinner(fmt.Sprintf("Deleting connector %s...", name))
			spin.Start()
			err := newServerlessClient(cmd, logger).DeleteOidcConnector(cmd.Context(), name)
			spin.Stop()
			if err != nil {
				return err
			}

			return output.Print(cmdutil.FormatFor(cmd), oidcConnectorDeletedResult{Name: name})
		},
	}
}
