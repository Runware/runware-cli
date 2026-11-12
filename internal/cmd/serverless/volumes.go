package serverless

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/oapi-codegen/nullable"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

const (
	descriptionFlag  = "description"
	volumesListUse   = "list"
	descriptionField = "description"
	// volumesPageSize is the page size the list commands ask for while they
	// follow every page.
	volumesPageSize serverlessapi.Limit = 100
	// volumeFilesPageSize is the largest page listVolumeFiles serves.
	volumeFilesPageSize serverlessapi.VolumeFileLimit = 1000
	// volumeFileListAttempts bounds how often volumes files starts again
	// because an upload changed the files between two pages.
	volumeFileListAttempts = 3
	// volumeSettingsCommand is the command that shows the organization's
	// access and limits.
	volumeSettingsCommand = "runware serverless volumes settings"
)

var (
	errNothingToUpdateOnVolume = errors.New("nothing to update; pass --description <text> or --clear description")
	errRepeatedCursor          = errors.New("the API returned the same page cursor twice; stopped listing")
	errVolumeFilesKeptChanging = errors.New("the Volume's files kept changing while they were listed; retry once uploads to it finish")
)

// newVolumesCmd returns the "serverless volumes" command group for Shared
// Volumes. It stays hidden until Shared Volumes are released.
func newVolumesCmd(logger *log.Logger) *cobra.Command {
	cmd := stubGroup("volumes", "Manage Shared Volumes")
	cmd.Hidden = true
	cmd.Long = `Manage Shared Volumes: named, durable sets of files that apps mount.

A Volume belongs to one environment, and only apps in that environment mount
it. Every command here except 'settings' takes --env to select that
environment, else the default environment (RUNWARE_ENV, else the config's) or
production.

The lifecycle:

  1. 'volumes create' makes an empty Volume. No app is needed.
  2. 'volumes put' uploads files into it. Uploads are additive: they add or
     replace the paths they name and keep every other file.
  3. 'deploy --mount' mounts it into an app.

A Volume has one set of files, its latest files. Workers mount the latest
files when they start; running workers keep theirs until they stop. Rolling an
app back does not bring back older files. To start clean, create a new Volume.

A Shared Volume is not the node-local folder 'deploy --volume' declares: that
folder lives on one machine, while a Volume is stored by the platform and
mounted wherever the app's workers run.

Deleting a Volume is logical: it stops new mounts at once, and the platform
reclaims the stored bytes later.`
	cmd.AddCommand(
		newVolumesListCmd(logger),
		newVolumesCreateCmd(logger),
		newVolumesShowCmd(logger),
		newVolumesUpdateCmd(logger),
		newVolumesDeleteCmd(logger),
		newVolumesReferencesCmd(logger),
		newVolumesFilesCmd(logger),
		newVolumesSettingsCmd(logger),
	)
	return cmd
}

// addVolumeEnvironmentFlag registers --env on a Volume command that addresses
// one environment.
func addVolumeEnvironmentFlag(cmd *cobra.Command, logger *log.Logger) {
	addEnvironmentFlag(cmd, cmd.Flags(), logger, "Environment the Volume belongs to "+environmentFlagDefault)
}

func newVolumesListCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   volumesListUse,
		Short: "List Volumes",
		Long: `List every Volume in one environment, newest first, following every page.

The table shows sizes in binary units and short ids; --format json or yaml
prints the API objects with sizes in bytes and full ids.`,
		Example: `  # list the Volumes of the default environment (production unless set)
  runware serverless volumes list

  # list the staging Volumes as JSON
  runware serverless volumes list --env staging --format json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			spin := cmdutil.NewSpinner("Fetching Volumes...")
			spin.Start()

			client := newServerlessClient(cmd, logger)
			volumes, err := listAllVolumes(cmd.Context(), client)
			spin.Stop()
			if err != nil {
				return volumeError(cmd, client, err, volumeOperation{
					action: "Listing Volumes",
				})
			}

			return output.Print(cmdutil.FormatFor(cmd), volumesResult(volumes))
		},
	}
	addVolumeEnvironmentFlag(cmd, logger)
	return cmd
}

func newVolumesCreateCmd(logger *log.Logger) *cobra.Command {
	var description string

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a Volume",
		Long: `Create an empty Volume in one environment.

The name is unique among the environment's Volumes and never changes. Fill the
Volume with 'volumes put', then mount it with 'deploy --mount'.`,
		Example: `  # create a Volume in the default environment (production unless set)
  runware serverless volumes create model-weights

  # create one in staging with a description
  runware serverless volumes create model-weights --env staging --description "SDXL base and refiner weights"`,
		Args: volumeNameArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			body := serverlessapi.VolumeCreate{
				Name: args[0],
			}
			if cmd.Flags().Changed(descriptionFlag) {
				body.Description = &description
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Creating Volume %s...", body.Name))
			spin.Start()

			client := newServerlessClient(cmd, logger)
			volume, err := client.CreateVolume(cmd.Context(), body)
			spin.Stop()
			if err != nil {
				return volumeError(cmd, client, err, volumeOperation{
					action:    "Creating a Volume",
					name:      body.Name,
					creates:   true,
					unchanged: "nothing was created",
				})
			}

			return output.Print(cmdutil.FormatFor(cmd), volumeResult{
				volume: *volume,
			})
		},
	}
	cmd.Flags().StringVar(&description, descriptionFlag, "", "Free-text description, at most 1024 characters")
	addVolumeEnvironmentFlag(cmd, logger)
	return cmd
}

func newVolumesShowCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show a Volume",
		Long: `Show a Volume: its size, file count, the digest of its latest file list,
and the app versions that use it.

The digest changes exactly when the latest files change, so comparing it tells
whether they did. It is not an id: nothing accepts it.

--format json or yaml prints the API's Volume object; 'volumes references'
prints the uses on their own.`,
		Example: `  runware serverless volumes show model-weights

  # the staging Volume as JSON
  runware serverless volumes show model-weights --env staging --format json`,
		Args: volumeNameArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			spin := cmdutil.NewSpinner(fmt.Sprintf("Fetching Volume %s...", name))
			spin.Start()

			client := newServerlessClient(cmd, logger)
			volume, err := client.GetVolume(cmd.Context(), name)
			if err != nil {
				spin.Stop()
				return volumeError(cmd, client, err, volumeOperation{
					action: "Reading a Volume",
					name:   name,
				})
			}
			result := volumeResult{
				volume: *volume,
			}
			if cmdutil.FormatFor(cmd) == output.FormatTable {
				result.withUses = true
				result.references, err = client.GetVolumeReferences(cmd.Context(), name)
				if err != nil {
					result.referencesErr = referencesUnavailable(err)
				}
			}
			spin.Stop()

			return output.Print(cmdutil.FormatFor(cmd), result)
		},
	}
	addVolumeEnvironmentFlag(cmd, logger)
	return cmd
}

func newVolumesUpdateCmd(logger *log.Logger) *cobra.Command {
	var (
		description string
		clears      []string
	)

	cmd := &cobra.Command{
		Use:   "update <name>",
		Short: "Update a Volume's description",
		Long: `Change a Volume's description, the only thing about a Volume that changes
here. Its name and environment never change, and its files change only through
'volumes put'.

--clear description removes the description.`,
		Example: `  # replace the description
  runware serverless volumes update model-weights --description "SDXL weights, fp16"

  # remove it
  runware serverless volumes update model-weights --clear description`,
		Args: volumeNameArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			body, err := volumeUpdateBody(cmd, description, clears)
			if err != nil {
				return err
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Updating Volume %s...", name))
			spin.Start()

			client := newServerlessClient(cmd, logger)
			volume, err := client.UpdateVolume(cmd.Context(), name, body)
			spin.Stop()
			if err != nil {
				return volumeError(cmd, client, err, volumeOperation{
					action:    "Updating a Volume",
					name:      name,
					unchanged: "nothing was changed",
				})
			}

			return output.Print(cmdutil.FormatFor(cmd), volumeResult{
				volume: *volume,
			})
		},
	}
	cmd.Flags().StringVar(&description, descriptionFlag, "", "New free-text description, at most 1024 characters")
	addClearFlag(cmd, &clears, []string{descriptionField})
	addVolumeEnvironmentFlag(cmd, logger)
	return cmd
}

// volumeUpdateBody is the patch the update flags ask for: a description that
// replaces the current one, or null to clear it.
func volumeUpdateBody(cmd *cobra.Command, description string, clears []string) (serverlessapi.VolumeUpdate, error) {
	cleared, err := clearedFields(cmd.Flags(), clears, []string{descriptionField})
	if err != nil {
		return serverlessapi.VolumeUpdate{}, err
	}
	var body serverlessapi.VolumeUpdate
	switch {
	case len(cleared) > 0:
		body.Description = nullable.NewNullNullable[serverlessapi.VolumeDescription]()
	case cmd.Flags().Changed(descriptionFlag):
		body.Description = nullable.NewNullableWithValue(description)
	default:
		return serverlessapi.VolumeUpdate{}, errNothingToUpdateOnVolume
	}
	return body, nil
}

func newVolumesDeleteCmd(logger *log.Logger) *cobra.Command {
	var (
		yes   bool
		force bool
	)

	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a Volume",
		Long: `Delete a whole Volume: its latest files and any upload still open on it.
Individual files cannot be deleted.

The command reads the Volume's references first. A Volume that a current app
version, a deploy in progress or a running worker uses is not deleted: the
command prints those uses and fails. A Volume that only older app versions use
can be deleted, and those versions lose their rollback; the command names them
and asks before it goes ahead, and asks again if they change while it waits. The API checks the uses again when the delete
commits, and confirming never overrides a current use.

The deletion is logical. New mounts of the Volume stop at once, and the
platform reclaims its stored bytes later. A new Volume with the same name is a
different Volume, and never satisfies a mount of this one.

Confirmation is required unless --yes or --force is passed.`,
		Example: `  # delete a Volume (prompts for confirmation)
  runware serverless volumes delete model-weights

  # delete the staging Volume without a prompt
  runware serverless volumes delete model-weights --env staging --yes`,
		Args: volumeNameArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			return deleteVolume(cmd, logger, args[0], yes || force)
		},
	}
	addDeleteConfirmFlags(cmd, &yes, &force)
	addVolumeEnvironmentFlag(cmd, logger)
	return cmd
}

// deleteVolume deletes a Volume after reading its references: it refuses a
// Volume in use, and names the app versions a confirmed delete takes the
// rollback from before it asks, and again when they change while it asks. The
// references come first even without a terminal, so a script that forgot
// --yes still learns what uses the Volume.
func deleteVolume(cmd *cobra.Command, logger *log.Logger, name string, skipConfirm bool) error {
	if config.GetAPIKey() == "" {
		return transport.ErrNoAPIKey
	}
	in, errOut := cmd.InOrStdin(), cmd.ErrOrStderr()
	isTTY := stdinIsTerminal(in)

	client := newServerlessClient(cmd, logger)
	refs, err := readDeletableReferences(cmd, client, name)
	if err != nil {
		return err
	}

	var confirmRollbackLoss bool
	for {
		confirmRollbackLoss = len(refs.RollbackTargets) > 0
		if confirmRollbackLoss {
			_, _ = fmt.Fprintf(errOut, "Deleting Volume %s removes the rollback of %s.\n", name, describeVolumeReferences(refs.RollbackTargets))
		}
		if err := confirmDelete(deleteSubject(cmd, "Volume "+name), skipConfirm, in, errOut, isTTY, config.GetAPIKey()); err != nil {
			return err
		}
		if skipConfirm {
			break
		}
		// The prompt waits without a time limit, so the confirmed rollback
		// targets may be stale: read them again, and ask again on a change.
		var fresh *serverlessapi.VolumeReferences
		fresh, err = readDeletableReferences(cmd, client, name)
		if err != nil {
			return err
		}
		if sameVolumeReferences(fresh.RollbackTargets, refs.RollbackTargets) {
			break
		}
		refs = fresh
		_, _ = fmt.Fprintf(errOut, "What uses Volume %s changed while the prompt waited.\n", name)
	}

	spin := cmdutil.NewSpinner(fmt.Sprintf("Deleting Volume %s...", name))
	spin.Start()
	err = client.DeleteVolume(cmd.Context(), name, confirmRollbackLoss)
	spin.Stop()
	if err != nil {
		return volumeDeleteError(cmd, client, err, name)
	}

	return output.Print(cmdutil.FormatFor(cmd), volumeDeletedResult{
		Name:         name,
		Environment:  volumeEnvironment(cmd),
		LostRollback: nonNilReferences(refs.RollbackTargets),
	})
}

// readDeletableReferences reads what uses Volume name for a delete. A use
// that blocks the delete is printed and refused.
func readDeletableReferences(cmd *cobra.Command, client *serverlessapi.Client, name string) (*serverlessapi.VolumeReferences, error) {
	spin := cmdutil.NewSpinner(fmt.Sprintf("Reading what uses Volume %s...", name))
	spin.Start()
	refs, err := client.GetVolumeReferences(cmd.Context(), name)
	spin.Stop()
	if err != nil {
		return nil, volumeError(cmd, client, err, volumeOperation{
			action:    "Listing what uses a Volume, which a delete reads first,",
			name:      name,
			unchanged: "nothing was deleted",
		})
	}
	if len(refs.Blocking) > 0 {
		if err := output.Print(cmdutil.FormatFor(cmd), volumeReferencesResult(*refs)); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("volume %s is in use by %s, so nothing was deleted; remove it from those apps, wait for their deploys and workers to finish, and retry", name, describeVolumeReferences(refs.Blocking))
	}
	return refs, nil
}

// sameVolumeReferences reports whether a and b name the same app versions,
// in any order.
func sameVolumeReferences(a, b []serverlessapi.VolumeReference) bool {
	if len(a) != len(b) {
		return false
	}
	seen := make(map[serverlessapi.VolumeReference]struct{}, len(a))
	for _, r := range a {
		seen[r] = struct{}{}
	}
	for _, r := range b {
		if _, ok := seen[r]; !ok {
			return false
		}
	}
	return true
}

// volumeDeleteError explains a refused delete. The references were read
// before the delete, so a conflict means a use appeared in between.
func volumeDeleteError(cmd *cobra.Command, client *serverlessapi.Client, err error, name string) error {
	re, ok := errors.AsType[*transport.RunwareError](err)
	if ok && re.StatusCode == http.StatusConflict {
		refsCommand := serverlessCommand(volumeEnvironment(cmd), defaultEnvironment(cmd), "volumes", "references", name)
		switch re.ProblemType {
		case serverlessapi.ProblemVolumeInUse:
			return withMessage(re, fmt.Sprintf("Volume %s came into use after its references were read, so nothing was deleted; see '%s'", name, refsCommand))
		case serverlessapi.ProblemVolumeRollbackConfirmationRequired:
			return withMessage(re, fmt.Sprintf("an older app version started using Volume %s after its references were read, so nothing was deleted; retry to review it", name))
		}
	}
	return volumeError(cmd, client, err, volumeOperation{
		action:    "Deleting a Volume",
		name:      name,
		unchanged: "the Volume was not deleted",
	})
}

func newVolumesReferencesCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "references <name>",
		Short: "List what uses a Volume",
		Long: `List the app versions that use a Volume.

Current app versions, deploys in progress and running workers block a delete.
Older app versions do not, but a delete takes their rollback. The list is a
snapshot: 'volumes delete' reads it again, and the API checks again when the
delete commits.`,
		Example: `  runware serverless volumes references model-weights

  # as JSON, for a script
  runware serverless volumes references model-weights --format json`,
		Args: volumeNameArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			spin := cmdutil.NewSpinner(fmt.Sprintf("Reading what uses Volume %s...", name))
			spin.Start()

			client := newServerlessClient(cmd, logger)
			refs, err := client.GetVolumeReferences(cmd.Context(), name)
			spin.Stop()
			if err != nil {
				return volumeError(cmd, client, err, volumeOperation{
					action: "Listing what uses a Volume",
					name:   name,
				})
			}
			if len(refs.Blocking) == 0 && len(refs.RollbackTargets) == 0 && cmdutil.FormatFor(cmd) == output.FormatTable {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "No app version uses Volume %s.\n", name)
			}

			return output.Print(cmdutil.FormatFor(cmd), volumeReferencesResult(*refs))
		},
	}
	addVolumeEnvironmentFlag(cmd, logger)
	return cmd
}

func newVolumesFilesCmd(logger *log.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "files <name>",
		Short: "List a Volume's latest files",
		Long: `List every one of a Volume's latest files, ordered by path, following every
page.

The listing reads file metadata only: paths, sizes, modes and digests. There is
no download. When an upload changes the files between two pages, the command
starts again, so the list always comes from one file set.`,
		Example: `  runware serverless volumes files model-weights

  # every path, size in bytes and digest as JSON
  runware serverless volumes files model-weights --format json`,
		Args: volumeNameArg,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			spin := cmdutil.NewSpinner(fmt.Sprintf("Fetching the files of Volume %s...", name))
			spin.Start()

			client := newServerlessClient(cmd, logger)
			files, err := listAllVolumeFiles(cmd.Context(), client, name)
			spin.Stop()
			if err != nil {
				return volumeError(cmd, client, err, volumeOperation{
					action: "Listing a Volume's files",
					name:   name,
				})
			}
			if len(files.Data) == 0 && cmdutil.FormatFor(cmd) == output.FormatTable {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Volume %s has no files yet.\n", name)
			}

			return output.Print(cmdutil.FormatFor(cmd), volumeFilesResult(files))
		},
	}
	addVolumeEnvironmentFlag(cmd, logger)
	return cmd
}

func newVolumesSettingsCmd(logger *log.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "settings",
		Short: "Show the organization's Shared Volume access and limits",
		Long: `Show whether Shared Volumes are enabled for the organization, and the
effective limits that apply to every environment.

Settings belong to the organization, so this command takes no --env.`,
		Example: `  runware serverless volumes settings

  # limits in bytes, for a script
  runware serverless volumes settings --format json`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			spin := cmdutil.NewSpinner("Fetching Shared Volume settings...")
			spin.Start()

			settings, err := newServerlessClient(cmd, logger).GetVolumeSettings(cmd.Context())
			spin.Stop()
			if err != nil {
				return err
			}

			return output.Print(cmdutil.FormatFor(cmd), volumeSettingsResult(*settings))
		},
	}
}

// volumeNameArg takes one argument, a Volume name the API accepts.
func volumeNameArg(cmd *cobra.Command, args []string) error {
	if err := cobra.ExactArgs(1)(cmd, args); err != nil {
		return err
	}
	if !serverlessapi.VolumeNamePattern.MatchString(args[0]) {
		return serverlessapi.ErrVolumeName
	}
	return nil
}

// listAllVolumes follows every page of the client environment's Volumes.
func listAllVolumes(ctx context.Context, client *serverlessapi.Client) ([]serverlessapi.Volume, error) {
	volumes := []serverlessapi.Volume{}
	limit := volumesPageSize
	var cursor *serverlessapi.Cursor
	for {
		page, err := client.ListVolumes(ctx, serverlessapi.ListVolumesParams{
			Limit:  &limit,
			Cursor: cursor,
		})
		if err != nil {
			return nil, err
		}
		volumes = append(volumes, page.Data...)
		next, err := nextCursor(cursor, page.NextCursor)
		if err != nil || next == nil {
			return volumes, err
		}
		cursor = next
	}
}

// listAllVolumeFiles follows every page of a Volume's latest files. A page
// read from a different file list than the first means an upload completed
// in between, so the listing starts again, a bounded number of times.
func listAllVolumeFiles(ctx context.Context, client *serverlessapi.Client, name string) (serverlessapi.VolumeFilePage, error) {
	for range volumeFileListAttempts {
		files, consistent, err := listVolumeFilesOnce(ctx, client, name)
		if err != nil || consistent {
			return files, err
		}
	}
	return serverlessapi.VolumeFilePage{}, errVolumeFilesKeptChanging
}

// listVolumeFilesOnce reads every page of a Volume's latest files and reports
// whether they all came from the same file list.
func listVolumeFilesOnce(ctx context.Context, client *serverlessapi.Client, name string) (serverlessapi.VolumeFilePage, bool, error) {
	result := serverlessapi.VolumeFilePage{
		Data: []serverlessapi.VolumeFile{},
	}
	limit := volumeFilesPageSize
	var cursor *serverlessapi.Cursor
	for first := true; ; first = false {
		page, err := client.ListVolumeFiles(ctx, name, serverlessapi.ListVolumeFilesParams{
			Limit:  &limit,
			Cursor: cursor,
		})
		if err != nil {
			return serverlessapi.VolumeFilePage{}, false, err
		}
		if first {
			result.ManifestSha256 = page.ManifestSha256
		} else if !sameDigest(result.ManifestSha256, page.ManifestSha256) {
			return serverlessapi.VolumeFilePage{}, false, nil
		}
		result.Data = append(result.Data, page.Data...)
		next, err := nextCursor(cursor, page.NextCursor)
		if err != nil {
			return serverlessapi.VolumeFilePage{}, false, err
		}
		if next == nil {
			return result, true, nil
		}
		cursor = next
	}
}

// nextCursor is the cursor of the page after one read with current, or nil
// after the last page. A cursor the API repeats would loop forever.
func nextCursor(current, next *string) (*string, error) {
	if next == nil || *next == "" {
		return nil, nil
	}
	if current != nil && *current == *next {
		return nil, errRepeatedCursor
	}
	return next, nil
}

func sameDigest(a, b *string) bool {
	return (a == nil) == (b == nil) && (a == nil || *a == *b)
}

// referencesUnavailable is the short reason show gives when it cannot read a
// Volume's references.
func referencesUnavailable(err error) string {
	if re, ok := errors.AsType[*transport.RunwareError](err); ok && re.StatusCode == http.StatusNotImplemented {
		return "unavailable: not implemented by the API yet"
	}
	msg, _, _ := strings.Cut(err.Error(), "\n")
	return "unavailable: " + msg
}

// volumeEnvironment is the environment a Volume command addresses.
func volumeEnvironment(cmd *cobra.Command) string {
	return cmp.Or(commandEnvironment(cmd), serverlessapi.ProductionEnvironment)
}

// volumeOperation describes the command a Shared Volume refusal stopped.
type volumeOperation struct {
	// action names the operation as the start of a sentence.
	action string
	// name is the Volume the command addresses, or "" for list.
	name string
	// creates marks an operation whose 404 cannot mean an unknown Volume.
	creates bool
	// unchanged says what did not happen, for an operation that would change
	// something, or "".
	unchanged string
}

// volumeError turns a Shared Volume refusal into one line that says what to
// do next. Any other error passes through unchanged.
func volumeError(cmd *cobra.Command, client *serverlessapi.Client, err error, op volumeOperation) error {
	re, ok := errors.AsType[*transport.RunwareError](err)
	if !ok {
		return err
	}
	unchanged := ""
	if op.unchanged != "" {
		unchanged = "; " + op.unchanged
	}
	env := volumeEnvironment(cmd)
	switch re.StatusCode {
	case http.StatusNotFound:
		// An organization without beta access gets the 404 of an unknown
		// path on every Volume route but the settings, so the settings tell
		// the two apart. Without them the 404 stays as the API said it.
		settings, settingsErr := client.GetVolumeSettings(cmd.Context())
		switch {
		case settingsErr == nil && !settings.Available:
			return withMessage(re, "Shared Volumes are not enabled for this organization"+unchanged+"; ask Runware for beta access, and check it with '"+volumeSettingsCommand+"'")
		case settingsErr == nil && op.name != "" && !op.creates:
			return withMessage(re, fmt.Sprintf("no Volume named %s in environment %s%s; list them with '%s'", op.name, env, unchanged, serverlessCommand(env, defaultEnvironment(cmd), "volumes", "list")))
		}
	case http.StatusNotImplemented:
		return withMessage(re, op.action+" is not available yet: the API answered 501 Not Implemented"+unchanged+"; retry after the API release that adds it")
	case http.StatusConflict:
		switch re.ProblemType {
		case serverlessapi.ProblemVolumeNameTaken:
			return withMessage(re, fmt.Sprintf("a Volume named %s already exists in environment %s, or one by that name is still being deleted%s; choose another name", op.name, env, unchanged))
		case serverlessapi.ProblemVolumeLimitExceeded:
			return withMessage(re, "a Shared Volume limit is exceeded ("+re.Message+")"+unchanged+"; see the limits with '"+volumeSettingsCommand+"'")
		}
	}
	return err
}

// withMessage is a copy of re that says message instead.
func withMessage(re *transport.RunwareError, message string) error {
	out := *re
	out.Message = message
	return &out
}

// describeVolumeReferences names app versions as "img-gen v7 (current app
// version), img-gen v3".
func describeVolumeReferences(refs []serverlessapi.VolumeReference) string {
	parts := make([]string, len(refs))
	for i, r := range refs {
		parts[i] = fmt.Sprintf("%s v%d", r.AppId, r.AppVersionNumber)
		if r.Kind != serverlessapi.VolumeReferenceRollbackTarget {
			parts[i] += " (" + volumeReferenceKind(r.Kind) + ")"
		}
	}
	return strings.Join(parts, ", ")
}

// volumeReferenceKind is a reference kind in words.
func volumeReferenceKind(kind serverlessapi.VolumeReferenceKind) string {
	return strings.ReplaceAll(string(kind), "-", " ")
}

func nonNilReferences(refs []serverlessapi.VolumeReference) []serverlessapi.VolumeReference {
	if refs == nil {
		return []serverlessapi.VolumeReference{}
	}
	return refs
}
