package serverless

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

// newAppsConnectorsCmd returns the "serverless apps connectors" command group
// for the OIDC connectors attached to an application.
func newAppsConnectorsCmd(logger *log.Logger) *cobra.Command {
	cmd := stubGroup("connectors", "Manage the OIDC connectors attached to an application")
	cmd.Long = `Attach OIDC connectors to an application, detach them, and test the trust
setup of an attached connector.

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
		newAppsConnectorsTestCmd(logger),
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

			return output.Print(cmdutil.FormatFor(cmd), appOidcConnectorsResult(connectors))
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
getting a token. An application that is not live picks the connector up on its
next rollout.

` + rolloutInProgressHelp(connectorWaitMessage) + `

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

			return output.Print(cmdutil.FormatFor(cmd), appOidcConnectorResult(*connector))
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
worker already holds stays valid until it expires, at most an hour. An
application that is not live loses the connector on its next rollout.

` + rolloutInProgressHelp(connectorWaitMessage),
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

// connectorTestMaxWait is how long test waits for a result by default. The API
// ends every test within two minutes, so this is a little more.
const connectorTestMaxWait = 150 * time.Second

func newAppsConnectorsTestCmd(logger *log.Logger) *cobra.Command {
	var (
		timeout      time.Duration
		pollInterval time.Duration
	)

	cmd := &cobra.Command{
		Use:   "test <appId> <connector>",
		Short: "Test the trust setup of an attached OIDC connector",
		Long: `Test the trust setup between an application and a connector's cloud without
deploying any code, and print the outcome. A test needs an aws or gcp connector
that is attached to the application in this environment.

The platform runs a short-lived probe as a worker of the application. The probe
gets the application's token as a worker does and exchanges it with the cloud:
AWS AssumeRoleWithWebIdentity, or Google STS and, when the connector names a
service account, impersonation of it. It obtains real, short-lived credentials
and discards them. No token or credential is returned, stored or printed. On AWS
the session is named runware-connector-test-<test id>, so a test shows in
CloudTrail.

The outcome is one of:
  ok              the cloud accepted the token; the assumed role or impersonated
                  service account is printed
  cloud_error     the cloud refused the token; its code, message and a
                  troubleshooting hint are printed, with the stage it refused at
  platform_error  the platform could not run the test; the reason and what went
                  wrong are printed (with the issuer's code for issuer_refused),
                  and they say nothing about the trust setup

The command exits non-zero unless the outcome is ok. A result normally arrives
within a minute and always within two; --timeout stops waiting sooner.

One test of an application's connectors runs at a time in an environment, and an
organization may start 20 tests an hour (60 that end in platform_error). Past
either the API refuses, and the refusal says when to retry.`,
		Example: `  runware serverless apps connectors test my-app aws-prod

  # the staging copy of the application
  runware serverless apps connectors test my-app aws-prod --env staging

  # machine-readable outcome
  runware serverless apps connectors test my-app aws-prod -F json`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			app, name := args[0], args[1]
			if timeout <= 0 {
				return fmt.Errorf("--timeout must be positive")
			}

			client := newServerlessClient(cmd, logger)
			spin := cmdutil.NewSpinner(fmt.Sprintf("Starting a test of connector %s on %s...", name, app))
			spin.Start()

			// The start is outside the --timeout budget: a start cut off after the
			// API accepted it would leave a running test this command never names.
			test, err := client.StartAppOidcConnectorTest(cmd.Context(), app, name)
			if err != nil {
				spin.Stop()
				choice := environmentFor(cmd)
				listCmd := serverlessCommand(choice.name, choice.defaultEnvironment, "apps", "connectors", "list", app)
				return connectorTestStartError(err, app, name, listCmd)
			}
			if test.Status == serverlessapi.OidcConnectorTestStatusPending {
				waitCtx, cancel := context.WithTimeout(cmd.Context(), timeout)
				defer cancel()
				id := test.Id.String()
				spin.SetMessage(fmt.Sprintf("Waiting for the result of test %s...", id))
				test, err = client.WaitAppOidcConnectorTest(waitCtx, app, name, test, pollInterval)
				if err != nil {
					spin.Stop()
					return connectorTestWaitErr(waitCtx, err, id, timeout)
				}
			}
			spin.Stop()

			if err := output.Print(cmdutil.FormatFor(cmd), oidcConnectorTestResult(*test)); err != nil {
				return err
			}
			return connectorTestFailedErr(name, test)
		},
	}

	cmd.Flags().DurationVar(&timeout, "timeout", connectorTestMaxWait, "Maximum time to wait for the result")
	cmd.Flags().DurationVar(&pollInterval, "poll-interval", 2*time.Second, "Polling interval when waiting for the result")
	return cmd
}

// connectorTestStartError says what a refused start means to the person at the
// terminal. The API's own detail stays on the message, and the error remains a
// *transport.RunwareError so the structured output and the status survive.
func connectorTestStartError(err error, app, name, listCmd string) error {
	re, ok := errors.AsType[*transport.RunwareError](err)
	if !ok {
		return err
	}
	var hint string
	switch re.StatusCode {
	case http.StatusNotFound:
		hint = fmt.Sprintf("connector %s was not found on %s: check the application id and that the connector is attached to it in this environment ('%s')", name, app, listCmd)
	case http.StatusConflict:
		hint = fmt.Sprintf("a connector test of %s is already running in this environment: wait for it to end, within two minutes, and run the test again", app)
	case http.StatusTooManyRequests:
		hint = "the organization has started as many connector tests as an hour allows"
	default:
		return err
	}
	out := *re
	out.Message = hint + "\n" + re.Message
	return &out
}

// connectorTestWaitError is a wait that ended before its test did. It names
// the test, which keeps running on the platform until it ends. It is a
// cmdutil.ResumableError, so an interrupted run still prints it, and it
// unwraps to its cause, so an interrupt still reads as one.
type connectorTestWaitError struct {
	msg   string
	cause error
}

var _ cmdutil.ResumableError = (*connectorTestWaitError)(nil)

func (e *connectorTestWaitError) Error() string { return e.msg }

// Resumable marks the error as one the CLI prints even when interrupted.
func (*connectorTestWaitError) Resumable() {}

func (e *connectorTestWaitError) Unwrap() error { return e.cause }

// connectorTestWaitErr names the test a wait gave up on. An API error keeps
// its type, with the note on its message, so its structured output and status
// survive.
func connectorTestWaitErr(waitCtx context.Context, err error, id string, timeout time.Duration) error {
	note := fmt.Sprintf("test %s keeps running and ends within two minutes of its start", id)
	if re, ok := errors.AsType[*transport.RunwareError](err); ok {
		out := *re
		out.Message = re.Message + "\n" + note
		return &connectorTestWaitError{msg: out.Message, cause: &out}
	}
	msg := waitTimeoutErr(waitCtx, err, "the connector test", timeout).Error() + "; " + note
	return &connectorTestWaitError{msg: msg, cause: err}
}

// connectorTestFailedErr is the exit status of a finished test: nil for ok, and
// an error for every other outcome. The outcome itself is printed on stdout.
func connectorTestFailedErr(name string, test *serverlessapi.OidcConnectorTest) error {
	switch test.Status {
	case serverlessapi.OidcConnectorTestStatusOk:
		return nil
	case serverlessapi.OidcConnectorTestStatusCloudError:
		return fmt.Errorf("the cloud refused the token of connector %s%s", name, connectorTestErrorDetail(test))
	case serverlessapi.OidcConnectorTestStatusPlatformError:
		reason := ""
		if test.Reason != nil {
			reason = ": " + string(*test.Reason)
		}
		return fmt.Errorf("the platform could not test connector %s%s; this says nothing about its trust setup", name, reason)
	default:
		return fmt.Errorf("connector %s test ended with unexpected status %q", name, test.Status)
	}
}

func connectorTestErrorDetail(test *serverlessapi.OidcConnectorTest) string {
	detail := ""
	if test.Code != nil && *test.Code != "" {
		detail = ": " + *test.Code
	}
	if test.Stage != nil && *test.Stage != "" {
		detail += " at " + string(*test.Stage)
	}
	return detail
}
