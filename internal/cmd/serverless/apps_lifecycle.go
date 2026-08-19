package serverless

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	errDeleteCancelled    = errors.New("delete cancelled")
	errDeleteNeedsConfirm = errors.New("delete requires confirmation; re-run with --yes or --force")
)

func newAppsStopCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "stop <appId>",
		Short: "Stop a serverless application",
		Long: `Stop a running serverless application.

The server accepts the stop and returns immediately with status stopping.
Worker drain is asynchronous; this command does not wait until the application
is stopped. The application must be active.`,
		Example: `  # stop a running application
  runware serverless apps stop my-app`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLifecycle(cmd, logger, args[0], "Stopping", (*serverlessapi.Client).StopDeployment)
		},
	}
}

func newAppsResumeCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "resume <appId>",
		Short: "Resume a stopped serverless application",
		Long: `Resume a stopped serverless application.

The server accepts the resume and returns immediately with status initializing.
Worker start is asynchronous; this command does not wait until the application
is active. The application must be stopped.`,
		Example: `  # resume a stopped application
  runware serverless apps resume my-app`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLifecycle(cmd, logger, args[0], "Resuming", (*serverlessapi.Client).ResumeDeployment)
		},
	}
}

func newAppsDeleteCmd(logger *log.Logger) *cobra.Command {
	var (
		yes   bool
		force bool
	)

	cmd := &cobra.Command{
		Use:   "delete <appId>",
		Short: "Delete a serverless application",
		Long: `Soft-delete a serverless application.

The server accepts the delete and returns immediately with status deleting.
Router removal and worker drain are asynchronous; this command does not wait
until the application is deleted.

Confirmation is required unless --yes or --force is passed.`,
		Example: `  # delete an application (prompts for confirmation)
  runware serverless apps delete my-app

  # skip the confirmation prompt
  runware serverless apps delete my-app --yes`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			if err := confirmDelete(id, yes || force, cmd.InOrStdin(), cmd.ErrOrStderr(), stdinIsTerminal(cmd.InOrStdin()), config.GetAPIKey()); err != nil {
				return err
			}
			return runLifecycle(cmd, logger, id, "Deleting", (*serverlessapi.Client).DeleteDeployment)
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Skip the confirmation prompt")
	cmd.Flags().BoolVar(&force, "force", false, "Skip the confirmation prompt")
	return cmd
}

type lifecycleAction func(*serverlessapi.Client, context.Context, string) (*serverlessapi.Deployment, error)

func runLifecycle(cmd *cobra.Command, logger *log.Logger, id, verb string, action lifecycleAction) error {
	spin := cmdutil.NewSpinner(fmt.Sprintf("%s application %s...", verb, id))
	spin.Start()

	client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
	dep, err := action(client, cmd.Context(), id)
	if err != nil {
		spin.Stop()
		return err
	}
	spin.Stop()

	return output.Print(cmdutil.FormatFor(cmd), deploymentResult(*dep))
}

// confirmDelete fails closed without an API key so a prompt cannot succeed
// and then fail with ErrNoAPIKey. skip (--yes/--force) bypasses the prompt.
func confirmDelete(appID string, skip bool, in io.Reader, out io.Writer, isTTY bool, apiKey string) error {
	if apiKey == "" {
		return transport.ErrNoAPIKey
	}
	if skip {
		return nil
	}
	if !isTTY {
		return errDeleteNeedsConfirm
	}

	_, _ = fmt.Fprintf(out, "Delete application %s? [y/N] ", appID)
	scanner := bufio.NewScanner(in)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("read confirmation: %w", err)
		}
		return errDeleteCancelled
	}
	switch strings.ToLower(strings.TrimSpace(scanner.Text())) {
	case "y", "yes":
		return nil
	default:
		return errDeleteCancelled
	}
}

func stdinIsTerminal(in io.Reader) bool {
	f, ok := in.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}
