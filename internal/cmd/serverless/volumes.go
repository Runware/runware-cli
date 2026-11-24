package serverless

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/coredump"
	"github.com/runware/runware-cli/internal/output"
	"github.com/runware/runware-cli/internal/volumes/keypair"
	"github.com/runware/runware-cli/internal/volumes/put"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const (
	toFlag = "to"
	// volumesStateDir is the directory under the CLI config directory that
	// holds the statement sequence record, the hash cache and the upload
	// journals.
	volumesStateDir = "volumes"
	// volumeSlugUploadNotOpen is the problem type of a session that was
	// completed, aborted or reached its deadline.
	volumeSlugUploadNotOpen = "volume-upload-not-open"
)

// Seams the command tests replace: the statement keys, whose verifier in a
// release holds only the platform's keys, and the part transport, which sends
// only over HTTPS, for the production API only to the platform's domain, and
// logs each part's number, status and size under --debug.
var (
	newVolumeKeys    = newVerifiedVolumeKeys
	newPartTransport = func(serverlessBaseURL string, logger *slog.Logger) put.Transport {
		return newHTTPPartTransport(nil, serverlessBaseURL, logger)
	}
)

// newHTTPPartTransport sends parts with client (nil for the default) and, when
// serverlessBaseURL is the production API, only to the platform's domain.
func newHTTPPartTransport(client *http.Client, serverlessBaseURL string, logger *slog.Logger) *put.HTTPTransport {
	return put.NewHTTPTransport(client).
		WithLogger(logger).
		WithTargetCheck(func(target string) bool {
			return keypair.AllowsUploadTarget(serverlessBaseURL, target)
		})
}

// newVerifiedVolumeKeys verifies statements with the release keys for the
// platform at serverlessBaseURL, recording accepted sequences in stateDir.
// A release ships no keys for the production host yet, so every statement
// from it is refused: the volumes group must stay hidden until the production
// verification keys ship.
func newVerifiedVolumeKeys(serverlessBaseURL, stateDir string) (put.Keys, error) {
	verifier, err := keypair.NewVerifier(serverlessBaseURL, stateDir)
	if err != nil {
		return nil, err
	}
	return put.NewKeys(verifier), nil
}

// newVolumesCmd returns the "serverless volumes" command group for Shared
// Volumes. It is hidden, and every command in it inherits that. Keep it
// hidden until the production verification keys ship (see
// newVerifiedVolumeKeys).
func newVolumesCmd(logger *log.Logger) *cobra.Command {
	cmd := stubGroup("volumes", "Manage Shared Volumes")
	cmd.Hidden = true
	cmd.AddCommand(newVolumesPutCmd(logger))
	return cmd
}

func newVolumesPutCmd(logger *log.Logger) *cobra.Command {
	var to string
	cmd := &cobra.Command{
		Use:     "put <name> <local path>...",
		Short:   put.HelpShort,
		Long:    put.HelpLong,
		Example: put.HelpExample,
		Args:    cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// Before anything that could hold key material exists.
			if err := coredump.Disable(); err != nil {
				return err
			}
			organization, _ := config.GetOrganization()
			configDir := config.ConfigDir()
			if configDir == "" {
				return errors.New("the CLI config directory is not set")
			}
			baseURL := config.GetServerlessBaseURL()
			keys, err := newVolumeKeys(baseURL, filepath.Join(configDir, volumesStateDir))
			if err != nil {
				return err
			}
			client := newServerlessClient(cmd, logger)
			volume := put.VolumeRef{
				Name:        args[0],
				Environment: commandEnvironment(cmd),
			}
			stderr := cmd.ErrOrStderr()
			summary, err := put.Run(cmd.Context(), put.Options{
				Volume:       volume,
				Organization: organization,
				Paths:        args[1:],
				To:           to,
				ConfigDir:    configDir,
				API:          client.VolumeUploads(),
				Keys:         keys,
				Transport:    newPartTransport(baseURL, slog.New(logger)),
				Progress:     stderr,
				ProgressTTY:  isTerminal(stderr),
			})
			if err != nil {
				return putError(volume, err)
			}
			return printPutSummary(cmd.OutOrStdout(), cmdutil.FormatFor(cmd), summary)
		},
	}
	cmd.Flags().StringVar(&to, toFlag, "", "Directory in the Volume the paths land under (default the Volume root)")
	addEnvironmentFlag(cmd, cmd.Flags(), logger, "Environment the Volume belongs to "+environmentFlagDefault)
	return cmd
}

// printPutSummary writes the summary as JSON or YAML, or as text with one line
// per file, so every format carries the same values.
func printPutSummary(w io.Writer, format output.Format, s put.Summary) error {
	if format == output.FormatJSON || format == output.FormatYAML {
		return output.PrintTo(w, format, s)
	}
	if err := s.WriteText(w); err != nil {
		return err
	}
	var b strings.Builder
	for _, f := range s.Files {
		fmt.Fprintf(&b, "  %-8s %s  %d bytes  %d sent  sha256 %s\n", f.Action, f.Path, f.Size, f.BytesSent, f.SHA256)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// putError turns the answers the engine passes through as they are into one
// actionable line, keeping the engine's error, less the error of a failed
// abort, which stays only as text in the line. Only a Volumes API answer maps
// on its status: a refused part comes from the upload endpoint, where a 404
// says nothing about the Volume. A 404 about one file (its completion, its
// part tokens) is about that file or session, not the Volume. A Volumes API
// answer that caused the failure also reaches cmdutil.PrintError as a
// *transport.RunwareError, which prints it like any other serverless error:
// JSON and YAML get the API's code and problem type, and the text format the
// code, or for a 401 or 403 only the CLI's auth hint.
func putError(volume put.VolumeRef, err error) error {
	cause := putCause(err)
	_, abortFailed := errors.AsType[*put.AbortFailedError](err)
	explained := explainPutError(volume, err)
	a, ok := errors.AsType[*serverlessapi.VolumeAPIError](cause)
	if (!ok || a.API == nil) && !abortFailed {
		return explained
	}
	failed := &putFailedError{
		line:  explained.Error(),
		cause: cause,
	}
	if ok && a.API != nil {
		// The RunwareError's message is the API's alone; keep the whole
		// line, which names the file and step that failed and any hint
		// above. The text format replaces it with the hint on an auth error,
		// as it does for every serverless command.
		api := *a.API
		api.Message = failed.line
		failed.api = &api
	}
	return failed
}

// putCause is the failure in err, without the note about a failed abort,
// whose API answer is about the session rather than the failure.
func putCause(err error) error {
	if af, ok := errors.AsType[*put.AbortFailedError](err); ok {
		return af.Cause
	}
	return err
}

func explainPutError(volume put.VolumeRef, err error) error {
	cause := putCause(err)
	if p, ok := errors.AsType[*put.ProblemError](cause); ok && p.Slug == volumeSlugUploadNotOpen {
		return fmt.Errorf("the upload session is no longer open: it was completed, aborted or reached its deadline; run put again (%w)", err)
	}
	a, ok := errors.AsType[*serverlessapi.VolumeAPIError](cause)
	if !ok || a.Problem.Status != http.StatusNotFound {
		return err
	}
	if _, perFile := errors.AsType[*put.FileError](cause); perFile {
		return err
	}
	env := volume.Environment
	if env == "" {
		env = "production"
	}
	return fmt.Errorf("volume %q was not found in environment %s, or Shared Volumes is not enabled for the organization; check the name and --env (%w)", volume.Name, env, err)
}

// putFailedError is the whole line put prints, unwrapping only to the
// failure: a refused abort is a note in the line, so cmdutil.PrintError never
// takes the abort's API answer for the failure's. It unwraps to api first, so
// PrintError finds that RunwareError, whose message is the whole line, before
// the API's own one inside cause.
type putFailedError struct {
	line  string
	api   *transport.RunwareError
	cause error
}

func (e *putFailedError) Error() string { return e.line }

func (e *putFailedError) Unwrap() []error {
	if e.api == nil {
		return []error{e.cause}
	}
	return []error{e.api, e.cause}
}

// isTerminal reports whether w is a terminal.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	return ok && term.IsTerminal(int(f.Fd()))
}
