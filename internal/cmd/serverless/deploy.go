package serverless

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/google/uuid"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

const deploySourceChoice = "pass an entry file or --container"

var codeOnlyDeployFlags = []string{
	"src-dir",
	"base-image",
	"requirement",
}

// createOnlyDeployFlags apply to CreateApp only. On an existing app they are
// rejected so a leftover create invocation cannot wipe env or silently no-op.
var createOnlyDeployFlags = []string{
	"gpu-type",
	"name",
	"max-workers",
	"idle-ttl",
	"scaling-delay",
	"min-workers",
	"gpus-per-worker",
	"volume",
	"env",
	"env-file",
}

// deploySource is the packed archive's type plus the fields CreateApp needs
// once the upload publishes a sourceId.
type deploySource struct {
	sourceType   serverlessapi.AppSourceType
	baseImage    string
	modelFile    string
	requirements []string
}

func (s deploySource) appSource(sourceID uuid.UUID) (serverlessapi.AppSourceUpsert, error) {
	switch s.sourceType {
	case serverlessapi.AppSourceTypeContainer:
		return serverlessapi.NewContainerAppSource(serverlessapi.ContainerSource{
			SourceId: sourceID,
		})
	case serverlessapi.AppSourceTypeCode:
		return serverlessapi.NewCodeAppSource(serverlessapi.CodeSourceUpsert{
			BaseImage: s.baseImage,
			Codebase: serverlessapi.CodebaseSource{
				SourceId:  sourceID,
				ModelFile: s.modelFile,
			},
			Requirements: optionalStringSlice(s.requirements),
		})
	default:
		return serverlessapi.AppSourceUpsert{}, fmt.Errorf("unsupported source type %q", s.sourceType)
	}
}

func validateDeployArgs(cmd *cobra.Command, args []string, containerDir string) error {
	hasFile := len(args) == 1
	hasContainer := containerDir != ""
	if hasFile == hasContainer {
		if hasFile {
			return fmt.Errorf("%s, not both", deploySourceChoice)
		}
		return fmt.Errorf("%s", deploySourceChoice)
	}
	if !hasContainer {
		return nil
	}
	for _, name := range codeOnlyDeployFlags {
		if cmd.Flags().Changed(name) {
			return fmt.Errorf("--%s applies to code deploys only; omit it when using --container", name)
		}
	}
	return nil
}

func buildDeployArchive(srcDir, containerDir, baseImage string, requirements []string, args []string) ([]byte, deploySource, error) {
	if containerDir != "" {
		archive, err := packContainerDirectory(containerDir)
		if err != nil {
			return nil, deploySource{}, err
		}
		return archive, deploySource{
			sourceType: serverlessapi.AppSourceTypeContainer,
		}, nil
	}

	archive, modelFile, err := packDirectory(srcDir, args[0])
	if err != nil {
		return nil, deploySource{}, err
	}
	return archive, deploySource{
		sourceType:   serverlessapi.AppSourceTypeCode,
		baseImage:    baseImage,
		modelFile:    modelFile,
		requirements: requirements,
	}, nil
}

func newDeployCmd(logger *log.Logger) *cobra.Command {
	var (
		id            string
		name          string
		maxWorkers    int32
		idleTTL       int32
		scalingDelay  int32
		baseImage     string
		gpuType       string
		requirements  []string
		minWorkers    int32
		gpusPerWorker int32
		srcDir        string
		containerDir  string
		volumes       []string
		envVars       []string
		envFiles      []string
		wait          bool
		pollInterval  time.Duration
	)

	cmd := &cobra.Command{
		Use:   "deploy [file]",
		Short: "Create or update a serverless application",
		Long: `Create or update a serverless application from Python code or a container source.

A first deploy with a new --id creates the application. A later deploy with the
same --id uploads a new source, records version N+1, and rolls it when the
build is ready. Create-only flags (--gpu-type, worker settings, --volume,
--env, --env-file, --name) apply only to create; passing them when the
application already exists is an error. Change workers with 'apps scale' and
environment with 'apps env'. A source update on a stopped application is 409.

A code deploy takes a Python entry file. The whole source directory is zipped
and submitted as the application source, so the entry file can import its own
modules and read its own data files. That directory is the working directory
unless --src-dir says otherwise.

The entry file must live inside the source directory. A relative path is resolved
inside it; an absolute path is taken as given.

A container deploy takes --container pointing at a directory whose root contains
Dockerfile and container.yaml (plus any build-context files the Dockerfile
copies). The directory is zipped and uploaded as source type container. Runware
builds a hosted wrapper image from that archive; the version records a buildId,
not a customer image reference. Invalid container.yaml is rejected on create
(400 if it cannot be parsed, 422 if it breaks a rule). The app stays
initializing until that first build rolls out. Pass --wait to poll until the
application is active or failed. A successful wait is not a live worker:
minWorkers=0 stays scaled to zero until the first invoke.

--container cannot be combined with an entry file, --src-dir, --base-image, or
--requirement.

Exclude what the app does not need with a .runwareignore file at the root of the
source directory; it takes gitignore syntax. A .gitignore is NOT consulted --
what a project keeps out of version control is a different question from what it
ships. Either way .env files are never uploaded, and neither are .git,
__pycache__, .venv, node_modules or the usual build and tool caches.

Pass --env or --env-file on create to set the application's initial environment.
Change a variable afterwards with 'apps env set' or 'apps env unset': a change
records a new version with the same image and rolls the workload when the app
is active, initializing, or failed and its image is deployable. A stopped or
stopping app applies it on resume. A write during an in-flight rollout
returns 409 and does not store the value. Prefer --env-file for anything
secret: a value passed as --env is visible in the process list and recorded
in shell history.

Anything the app downloads at runtime belongs on a --volume. The app runs in a
sandbox whose filesystem is part of the checkpointed state, so an unmounted
download is copied into every checkpoint and fetched again on every cold start.
A volume keeps it out of both.

Worker settings are supplied via flags on create. Endpoints are derived
server-side from the SDK (code) or from container.yaml (container).

A code app's endpoint path is its handler's method name with underscores turned
into hyphens, so renaming a method moves a public endpoint and 404s its callers.
Updating an existing application with --wait reports what the deploy did to the
endpoint set once the rollout lands. It is a report, not a gate: renaming an
endpoint on purpose is allowed. A first deploy with --wait prints the endpoint
paths and an invoke example once the application is active.`,
		Example: `  # deploy the current directory, with app.py as the entry point
  runware serverless deploy ./app.py --id my-app --gpu-type h100

  # update source on an existing application
  runware serverless deploy ./app.py --id my-app --wait

  # deploy a project that lives elsewhere; app.py is resolved inside --src-dir
  runware serverless deploy app.py --src-dir ~/projects/my-app --id my-app --gpu-type h100

  # an entry file in a subdirectory of the project
  runware serverless deploy src/app.py --src-dir ~/projects/my-app --id my-app --gpu-type h100

  # pass a token to the worker without putting it in the process list
  printf 'HF_TOKEN=%s' "$token" > .env.deploy
  runware serverless deploy model.py --id my-app --gpu-type l40s --env-file .env.deploy

  # keep downloaded model weights on persistent node-local storage
  runware serverless deploy model.py --id my-app --gpu-type l40s \
    --volume /root/.cache/huggingface

  # override worker settings and base image
  runware serverless deploy ./app.py --id my-app --name "My App" \
    --max-workers 2 --idle-ttl 120 --gpu-type h100 \
    --base-image python:3.12-slim --requirement torch

  # deploy a container source (Dockerfile + container.yaml at the directory root)
  runware serverless deploy --id my-app --gpu-type h100 --container ./wrapper

  # wait until the first rollout is active or failed
  runware serverless deploy ./app.py --id my-app --gpu-type h100 --wait`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateDeployArgs(cmd, args, containerDir); err != nil {
				return err
			}
			if err := validateGPUsPerWorkerFlag(cmd, gpusPerWorker); err != nil {
				return err
			}
			if name == "" {
				name = id
			}

			archive, source, err := buildDeployArchive(srcDir, containerDir, baseImage, requirements, args)
			if err != nil {
				return err
			}

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))

			update, err := existingApp(cmd.Context(), client, id)
			if err != nil {
				return err
			}
			if update {
				if err := validateUpdateDeployFlags(cmd); err != nil {
					return err
				}
			} else if err := validateCreateDeployGPU(gpuType); err != nil {
				return err
			}

			// The set the app serves now, to compare against what the new version
			// publishes. Only on an update we will wait for: a create has no
			// previous set, and without --wait this returns before the build that
			// decides the new one. A read failure costs the warning, not the deploy.
			var (
				endpointsBefore []string
				versionBefore   *uuid.UUID
				canCompare      bool
			)
			if update && wait {
				endpointsBefore, versionBefore, canCompare = endpointComparisonBase(cmd.Context(), client, id)
			}

			var (
				appVolumes *[]serverlessapi.AppVolume
				appEnv     *map[string]string
			)
			if !update {
				appVolumes, err = buildVolumes(volumes)
				if err != nil {
					return err
				}
				appEnv, err = buildEnvironmentVariables(envFiles, envVars)
				if err != nil {
					return err
				}
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Uploading source for %s...", id))
			spin.Start()
			sourceID, err := uploadSource(cmd.Context(), client, archive, source.sourceType)
			spin.Stop()
			if err != nil {
				return err
			}

			appSource, err := source.appSource(sourceID)
			if err != nil {
				return fmt.Errorf("build application source: %w", err)
			}

			var app *serverlessapi.App
			if update {
				spin = cmdutil.NewSpinner(fmt.Sprintf("Updating application %s...", id))
				spin.Start()
				app, err = client.UpdateApp(cmd.Context(), id, serverlessapi.AppUpdate{
					AppSource: &appSource,
				})
			} else {
				spin = cmdutil.NewSpinner(fmt.Sprintf("Creating application %s...", id))
				spin.Start()
				app, err = client.CreateApp(cmd.Context(), serverlessapi.AppCreate{
					AppId:                id,
					AppName:              name,
					AppSource:            appSource,
					Volumes:              appVolumes,
					EnvironmentVariables: appEnv,
					Configuration: serverlessapi.WorkerConfigCreate{
						MaxWorkers:       maxWorkers,
						IdleTtlSecs:      idleTTL,
						ScalingDelaySecs: scalingDelay,
						GpuType:          gpuType,
						MinWorkers:       optionalInt32Ptr(cmd, "min-workers", minWorkers),
						GpusPerWorker:    optionalInt32Ptr(cmd, "gpus-per-worker", gpusPerWorker),
					},
				})
				if isHTTPConflict(err) {
					update = true
					app, err = client.UpdateApp(cmd.Context(), id, serverlessapi.AppUpdate{
						AppSource: &appSource,
					})
				}
			}
			if err != nil {
				spin.Stop()
				return err
			}
			if wait && !serverlessapi.AppDeployTerminal(app.Status) {
				var lastStatus string
				app, err = waitForAppDeploy(cmd.Context(), client, app.AppId, pollInterval, func(appStatus, buildStatus string) {
					msg := deployWaitMessage(app.AppId, appStatus, buildStatus)
					if msg == lastStatus {
						return
					}
					lastStatus = msg
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s\n", msg)
					spin.SetMessage(msg)
				})
				if err != nil {
					spin.Stop()
					return err
				}
			}
			spin.Stop()

			// The endpoint rows are the outgoing version's until the submitted one
			// activates, and a source update on an already-active app answers
			// `active` throughout its build — so the wait above returns at once and
			// says nothing about whether this deploy landed. The pin is what moves
			// when it does.
			if canCompare {
				settled, err := waitForSubmittedVersion(cmd.Context(), client, app.AppId, versionBefore, pollInterval)
				if err == nil && activationMoved(versionBefore, settled.ActiveVersionId) {
					if paths, err := deployEndpointPaths(cmd.Context(), client, app.AppId); err == nil {
						reportEndpointSetChange(cmd.ErrOrStderr(), compareEndpointSets(endpointsBefore, paths))
					}
				}
			} else if wait && !update && app.Status == serverlessapi.AppStatusActive {
				if paths, err := deployEndpointPaths(cmd.Context(), client, app.AppId); err == nil {
					reportDeployEndpoints(cmd.ErrOrStderr(), app.AppId, paths)
				}
			}

			if err := output.Print(cmdutil.FormatFor(cmd), appResult(*app)); err != nil {
				return err
			}
			if !wait {
				return nil
			}
			return appFailedErr(cmd.Context(), client, app)
		},
	}

	cmd.Flags().StringVar(&srcDir, "src-dir", "", "Directory to package as the application source (default: the working directory; code deploys only)")
	cmd.Flags().StringVar(&containerDir, "container", "", "Directory whose root contains Dockerfile and container.yaml")
	cmd.Flags().StringArrayVar(&volumes, "volume", nil, "Absolute path inside the app backed by persistent node-local storage (repeatable)")
	cmd.Flags().StringArrayVar(&envVars, "env", nil, "Environment variable as KEY=VALUE (repeatable)")
	cmd.Flags().StringArrayVar(&envFiles, "env-file", nil, "File of KEY=VALUE lines to read environment variables from (repeatable)")
	cmd.Flags().StringVar(&id, "id", "", "Application ID (immutable, lowercase slug)")
	cmd.Flags().StringVar(&name, "name", "", "Display name (defaults to --id)")
	cmd.Flags().Int32Var(&maxWorkers, "max-workers", 1, "Maximum number of workers")
	cmd.Flags().Int32Var(&idleTTL, "idle-ttl", 60, "Idle TTL in seconds before scaling down")
	cmd.Flags().Int32Var(&scalingDelay, "scaling-delay", 10, "Scaling delay in seconds")
	cmd.Flags().StringVar(&baseImage, "base-image", "python:3.12-slim", "Builder base image (code deploys only; needs Python 3.12 or newer)")
	cmd.Flags().StringVar(&gpuType, "gpu-type", "", "GPU type ID (see 'serverless gpus'; required when creating)")
	cmd.Flags().StringArrayVar(&requirements, "requirement", nil, "Additional pip package to install (repeatable; code deploys only)")
	cmd.Flags().Int32Var(&minWorkers, "min-workers", 0, "Minimum number of workers")
	cmd.Flags().Int32Var(&gpusPerWorker, "gpus-per-worker", 1, "GPUs allocated per worker ("+gpusPerWorkerValuesText()+")")
	cmd.Flags().BoolVar(&wait, "wait", false, "Poll until the application is active or failed")
	cmd.Flags().DurationVar(&pollInterval, "poll-interval", 2*time.Second, "Polling interval when waiting for the application")

	if err := cmd.MarkFlagRequired("id"); err != nil {
		panic(err)
	}

	return cmd
}

func existingApp(ctx context.Context, client *serverlessapi.Client, id string) (bool, error) {
	_, err := client.GetApp(ctx, id)
	if err == nil {
		return true, nil
	}
	if isHTTPNotFound(err) {
		return false, nil
	}
	return false, err
}

func validateCreateDeployGPU(gpuType string) error {
	if gpuType == "" {
		return fmt.Errorf("--gpu-type is required when creating an application")
	}
	return nil
}

// gpusPerWorkerAllowed are the group sizes create and scale accept, in
// display order. The API rejects anything else with 422, which on deploy
// is after the archive upload.
var gpusPerWorkerAllowed = []int32{
	1,
	2,
	4,
	8,
}

func gpusPerWorkerValuesText() string {
	parts := make([]string, len(gpusPerWorkerAllowed))
	for i, n := range gpusPerWorkerAllowed {
		parts[i] = fmt.Sprint(n)
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + ", or " + parts[len(parts)-1]
}

func validateGPUsPerWorker(n int32) error {
	for _, allowed := range gpusPerWorkerAllowed {
		if n == allowed {
			return nil
		}
	}
	return fmt.Errorf("--gpus-per-worker must be %s", gpusPerWorkerValuesText())
}

func validateGPUsPerWorkerFlag(cmd *cobra.Command, n int32) error {
	if !cmd.Flags().Changed("gpus-per-worker") {
		return nil
	}
	return validateGPUsPerWorker(n)
}

func validateUpdateDeployFlags(cmd *cobra.Command) error {
	for _, name := range createOnlyDeployFlags {
		if cmd.Flags().Changed(name) {
			return fmt.Errorf("--%s applies to create only; %s", name, createOnlyDeployHint(name))
		}
	}
	return nil
}

func createOnlyDeployHint(name string) string {
	switch name {
	case "gpu-type", "max-workers", "idle-ttl", "scaling-delay", "min-workers", "gpus-per-worker":
		return "use 'runware serverless apps scale' to change worker configuration"
	case "env", "env-file":
		return "use 'runware serverless apps env' to change environment variables"
	case "volume":
		return "volumes are set at create time and cannot be changed here"
	default:
		return "omit it when updating an existing application"
	}
}

func optionalStringSlice(vals []string) *[]string {
	if len(vals) == 0 {
		return nil
	}
	return &vals
}

func optionalStringPtr(v string) *string {
	if v == "" {
		return nil
	}
	return &v
}

// optionalInt32Ptr returns a pointer when the flag was explicitly changed from
// its default, so omitted API fields keep existing or server-default values.
func optionalInt32Ptr(cmd *cobra.Command, name string, v int32) *int32 {
	if !cmd.Flags().Changed(name) {
		return nil
	}
	return &v
}

// optionalFlagStringPtr returns a pointer when the flag was explicitly set,
// including an empty string, so omitted flags stay omitted.
func optionalFlagStringPtr(cmd *cobra.Command, name, v string) *string {
	if !cmd.Flags().Changed(name) {
		return nil
	}
	return &v
}

// deployWaitMessage is the status line --wait prints while an image build is
// still running. buildStatus is empty when the build list cannot be read.
func deployWaitMessage(appID, appStatus, buildStatus string) string {
	if buildStatus == "" {
		return fmt.Sprintf("Waiting for application %s (%s)...", appID, appStatus)
	}
	return fmt.Sprintf("Waiting for application %s (%s, build %s)...", appID, appStatus, buildStatus)
}

// waitForAppDeploy polls until the app reaches a terminal status. Each
// non-terminal read reports the app status and the newest build status so a
// long image build is not a silent wait.
func waitForAppDeploy(
	ctx context.Context,
	client *serverlessapi.Client,
	appID string,
	interval time.Duration,
	report func(appStatus, buildStatus string),
) (*serverlessapi.App, error) {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	for {
		app, err := client.GetApp(ctx, appID)
		if err != nil {
			return nil, err
		}
		if serverlessapi.AppDeployTerminal(app.Status) {
			return app, nil
		}
		if report != nil {
			report(string(app.Status), latestBuildStatus(ctx, client, appID))
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// latestBuildStatus is the newest build's status. Newest first, as listBuilds
// returns them. An unreadable list is not a failure of the wait.
func latestBuildStatus(ctx context.Context, client *serverlessapi.Client, appID string) string {
	page, err := client.ListBuilds(ctx, appID, nil)
	if err != nil || len(page.Data) == 0 {
		return ""
	}
	return string(page.Data[0].Status)
}

func appFailedErr(ctx context.Context, client *serverlessapi.Client, app *serverlessapi.App) error {
	if app == nil {
		return nil
	}
	switch app.Status {
	case serverlessapi.AppStatusActive:
		return nil
	case serverlessapi.AppStatusFailed:
		if msg := latestBuildError(ctx, client, app.AppId); msg != "" {
			return fmt.Errorf("application %s failed: %s", app.AppId, msg)
		}
		return fmt.Errorf("application %s failed; inspect builds with 'runware serverless apps builds list %s'", app.AppId, app.AppId)
	default:
		return fmt.Errorf("application %s ended in status %s", app.AppId, app.Status)
	}
}

func latestBuildError(ctx context.Context, client *serverlessapi.Client, appID string) string {
	page, err := client.ListBuilds(ctx, appID, nil)
	if err != nil {
		return ""
	}
	for i := range page.Data {
		if page.Data[i].Error != nil && *page.Data[i].Error != "" {
			return *page.Data[i].Error
		}
	}
	return ""
}
