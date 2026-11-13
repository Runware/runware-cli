package serverless

import (
	"fmt"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

// newAppsConnectorsCmd returns the "serverless apps connectors" command group
// for the OIDC connectors attached to an application.
func newAppsConnectorsCmd(logger *log.Logger) *cobra.Command {
	cmd := stubGroup("connectors", "Manage the OIDC connectors attached to an application")
	cmd.Long = `Attach OIDC connectors to an application and detach them.

An application holds at most one connector of each kind (aws, gcp, generic),
and an attachment belongs to the copy of the application in one environment:
the one --env names, else the default environment (RUNWARE_ENV, else the
config's) or production. The same connector attached in another environment
has another subject.

Create connectors with 'serverless connectors create'.`
	cmd.AddCommand(
		newAppsConnectorsListCmd(logger),
		newAppsConnectorsAttachCmd(logger),
		newAppsConnectorsDetachCmd(logger),
	)
	return cmd
}

func newAppsConnectorsListCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   appConnectorsListUse,
		Short: "List the OIDC connectors attached to an application",
		Long: `List the OIDC connectors attached to an application in one environment, each
with the subject (sub) its workers' tokens carry. Use the subject in a
StringEquals trust condition.`,
		Example: `  runware serverless apps connectors list my-app

  # the staging copy of the application
  runware serverless apps connectors list my-app --env staging`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			app := args[0]

			spin := cmdutil.NewSpinner(fmt.Sprintf("Fetching connectors attached to %s...", app))
			spin.Start()
			connectors, err := newServerlessClient(cmd, logger).ListAppOidcConnectors(cmd.Context(), app)
			spin.Stop()
			if err != nil {
				return err
			}

			return output.Print(cmdutil.FormatFor(cmd), camelKeysTable{appOidcConnectorsResult(connectors)})
		},
	}
}

func newAppsConnectorsAttachCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "attach <appId> <connector>",
		Short: "Attach an OIDC connector to an application",
		Long: `Attach an OIDC connector to an application and print it with the subject its
workers' tokens carry and the subjectPattern that matches every application of
the organization in this environment.

Attaching rolls the application's live deployment, so its workers start
getting a token. An application that is not live, or already has a rollout in
progress, picks the connector up on its next rollout.

An application holds at most one connector of each kind. The env names a
connector sets on the worker are reserved while it is attached: attaching is
refused when the application already has a variable or secret under one.`,
		Example: `  runware serverless apps connectors attach my-app aws-prod

  # the staging copy of the application
  runware serverless apps connectors attach my-app aws-prod --env staging`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, name := args[0], args[1]

			spin := cmdutil.NewSpinner(fmt.Sprintf("Attaching connector %s to %s...", name, app))
			spin.Start()
			connector, err := newServerlessClient(cmd, logger).AttachAppOidcConnector(cmd.Context(), app, name)
			spin.Stop()
			if err != nil {
				return err
			}

			return output.Print(cmdutil.FormatFor(cmd), camelKeysTable{appOidcConnectorResult(*connector)})
		},
	}
}

func newAppsConnectorsDetachCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "detach <appId> <connector>",
		Short: "Detach an OIDC connector from an application",
		Long: `Detach an OIDC connector from an application. The connector itself remains.

Detaching rolls the application's live deployment, so its workers stop getting
a token. Running workers keep the connector until they roll, and a token a
worker already holds stays valid until it expires, at most an hour.`,
		Example: `  runware serverless apps connectors detach my-app aws-prod`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, name := args[0], args[1]

			spin := cmdutil.NewSpinner(fmt.Sprintf("Detaching connector %s from %s...", name, app))
			spin.Start()
			err := newServerlessClient(cmd, logger).DetachAppOidcConnector(cmd.Context(), app, name)
			spin.Stop()
			if err != nil {
				return err
			}

			return output.Print(cmdutil.FormatFor(cmd), oidcConnectorDetachedResult{AppID: app, Name: name})
		},
	}
}
