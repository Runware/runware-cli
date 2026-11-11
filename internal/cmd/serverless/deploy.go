package serverless

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	"github.com/google/uuid"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

const deploySourceChoice = "pass an entry file, --container, or --from"

var codeOnlyDeployFlags = []string{
	"src-dir",
	"base-image",
	"requirement",
}

// fromOnlyConflictFlags name a local source, which --from already supplies.
var fromOnlyConflictFlags = []string{
	"src-dir",
	"container",
}

const appNameFlag = "name"

// createOnlyDeployFlags apply to CreateApp only. On an existing app a flag is
// accepted when its value matches the app, and a changed value is rejected, so
// a leftover create invocation cannot wipe environment variables or silently
// no-op. --volume never matches: the app read does not return volumes.
var createOnlyDeployFlags = []string{
	"gpu-type",
	appNameFlag,
	"max-workers",
	"idle-ttl",
	"scaling-delay",
	"min-workers",
	"gpus-per-worker",
	"fallback-gpu-type",
	"min-available-workers",
	"available-workers-pct",
	"volume",
	"secret",
	envVarFlag,
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

func validateDeployArgs(cmd *cobra.Command, args []string, containerDir, from string) error {
	hasFile := len(args) == 1
	hasContainer := containerDir != ""
	hasFrom := from != ""

	chosen := 0
	for _, picked := range []bool{hasFile, hasContainer, hasFrom} {
		if picked {
			chosen++
		}
	}
	if chosen != 1 {
		if chosen == 0 {
			return fmt.Errorf("%s", deploySourceChoice)
		}
		return fmt.Errorf("%s, not more than one", deploySourceChoice)
	}

	if hasFrom {
		for _, name := range fromOnlyConflictFlags {
			if cmd.Flags().Changed(name) {
				return fmt.Errorf("--%s names a local source; --from takes the source from the archive", name)
			}
		}
		// The source type is inside the archive, so the code-only flags cannot be
		// judged yet. refuseCodeOnlyFlags runs again once the manifest is read.
		return nil
	}
	if !hasContainer {
		return nil
	}
	return refuseCodeOnlyFlags(cmd, "omit it when using --container")
}

// refuseCodeOnlyFlags rejects the flags a container deploy has no use for. A
// deploy that accepted and then discarded them would differ from the command
// the person wrote without saying so.
func refuseCodeOnlyFlags(cmd *cobra.Command, remedy string) error {
	for _, name := range codeOnlyDeployFlags {
		if cmd.Flags().Changed(name) {
			return fmt.Errorf("--%s applies to code deploys only; %s", name, remedy)
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
		id                  string
		name                string
		maxWorkers          int32
		idleTTL             int32
		scalingDelay        int32
		baseImage           string
		gpuType             string
		requirements        []string
		minWorkers          int32
		gpusPerWorker       int32
		fallbackGPUType     string
		minAvailableWorkers int32
		availableWorkersPct int32
		srcDir              string
		containerDir        string
		from                string
		volumes             []string
		secrets             []string
		envVars             []string
		envFiles            []string
		wait                bool
		timeout             time.Duration
		pollInterval        time.Duration
	)

	cmd := &cobra.Command{
		Use:   "deploy [file]",
		Short: "Create or update a serverless application",
		Long: `Create or update a serverless application from Python code or a container source.

The same --id can run once in each environment as a separate copy, with its
own versions, workers, variables and secrets. --env selects the copy this
deploy creates or updates, else the default environment (RUNWARE_ENV, else the
config's) or production.

A first deploy of an --id in an environment creates that copy. A later deploy
with the same --id and environment uploads a new source, records version N+1,
and rolls it when the build is ready. A source update on a stopped application
is 409.

Create-only flags are --gpu-type, --name, --max-workers, --idle-ttl,
--scaling-delay, --min-workers, --gpus-per-worker, --fallback-gpu-type,
--min-available-workers, --available-workers-pct, --volume, --secret,
--env-var and --env-file.

On an existing copy, a create-only flag is accepted when its value matches the
app, and a changed value is rejected; one error names every changed flag.
Change workers with 'apps scale', attach a secret later with 'secrets attach',
change environment variables with 'apps env-vars', and the display name with
'apps rename'. --volume is still always rejected on an existing app, because
the app read does not return volumes, so a repeated path cannot be told from a
changed one.

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
application is active or failed, and --timeout to bound that wait. A successful
wait is not a live worker:
minWorkers=0 stays scaled to zero until the first invoke.

--container cannot be combined with an entry file, --src-dir, --base-image, or
--requirement.

--from takes the URL of a packaged source: a zip whose root holds the app's
files and a runware.yaml recording the deploy options. The CLI fetches it,
reads that file, and deploys what it describes, so a published example deploys
in one command instead of a clone and a command. Flags still win over the
file, so the same archive goes to another GPU type or another --id without
being edited first. An --env-var or --env-file sets one key over the
manifest's and leaves the rest, while --volume, --requirement and --secret
replace the manifest's whole list. The archive supplies the source directory,
so --from cannot be combined with an entry file, --container, or --src-dir.

--from only creates. A manifest names an id, and deploying it over an
application that already has that id would roll the archive out as that
application's next version, running this code with the secrets, environment
variables and volumes it already has. So an existing id is refused: pass --id
to create a separate application, or unpack the archive and deploy the
directory to update the one that is there.

A runware.yaml carries no secret values. Its secrets name what the app needs,
and each one has to already exist in the environment. A manifest key this CLI
does not know is an error rather than a silent omission, which is what
configVersion is there to tell apart.

Exclude what the app does not need with a .runwareignore file at the root of the
source directory; it takes gitignore syntax. A .gitignore is NOT consulted --
what a project keeps out of version control is a different question from what it
ships. Either way .env files are never uploaded, and neither are .git,
__pycache__, .venv, node_modules or the usual build and tool caches.

Pass --env-var or --env-file on create to set the application's initial
environment variables. Change a variable afterwards with 'apps env-vars set'
or 'apps env-vars unset': a change records a new version with the same image
and rolls the workload when the app is active, initializing, or failed and its
image is deployable. A stopped or stopping app applies it on resume. A write
during an in-flight rollout returns 409 and does not store the value. Prefer
--env-file for anything secret: a value passed as --env-var is visible in the
process list and recorded in shell history.

Anything the app downloads at runtime belongs on a --volume. Volumes are set
at create and cannot be changed afterwards: a later deploy that passes
--volume is rejected. The app runs in a sandbox whose filesystem is part of
the checkpointed state, so an unmounted download is copied into every
checkpoint and fetched again on every cold start. A volume keeps it out of
both.

--secret NAME, or NAME=ENV_VAR, attaches an existing secret of the same
environment at create so the first rollout carries it. Repeat the flag for
more than one. Attach or detach later with 'secrets attach' and
'secrets detach'.

Worker settings are supplied via flags on create, including a fallback GPU
type and the idle-worker buffer. Endpoints are derived server-side from the
SDK (code) or from container.yaml (container).

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

  # create or update the staging copy of the application
  runware serverless deploy ./app.py --id my-app --gpu-type h100 --env staging

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

  # attach an existing secret and keep one idle worker warm
  runware serverless deploy ./app.py --id my-app --gpu-type h100 \
    --secret HF_TOKEN=HUGGING_FACE_HUB_TOKEN --min-available-workers 1

  # override worker settings and base image
  runware serverless deploy ./app.py --id my-app --name "My App" \
    --max-workers 2 --idle-ttl 120 --gpu-type h100 \
    --base-image python:3.12-slim --requirement torch

  # deploy a container source (Dockerfile + container.yaml at the directory root)
  runware serverless deploy --id my-app --gpu-type h100 --container ./wrapper

  # wait until the first rollout is active or failed
  runware serverless deploy ./app.py --id my-app --gpu-type h100 --wait --timeout 10m

  # deploy a packaged source, reading its id and options from its runware.yaml
  runware serverless deploy --from https://runware.ai/docs/serverless/examples/echo.zip

  # the same archive under another id and on another GPU type
  runware serverless deploy --from https://runware.ai/docs/serverless/examples/echo.zip \
    --id my-echo --gpu-type l40s`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := validateWaitFlags(cmd, wait, timeout); err != nil {
				return err
			}
			if err := validateDeployArgs(cmd, args, containerDir, from); err != nil {
				return err
			}

			var (
				manifest    deployManifest
				manifestEnv map[string]string
			)
			if from != "" {
				dir, cleanup, err := fetchSourceArchive(cmd.Context(), from)
				if err != nil {
					return err
				}
				defer cleanup()

				if manifest, err = readDeployManifest(dir); err != nil {
					return err
				}

				// The manifest's own values, for every flag the command line left
				// alone. A passed flag wins, which is what lets one packaged source
				// deploy on another GPU type without being edited first.
				//
				// Nothing here marks a flag as changed. The create-only flags are
				// refused on an update by exactly that test, and a deploy from a
				// packaged source only ever creates, so the manifest's own values
				// always reach a create.
				if manifest.Source.Type == string(serverlessapi.AppSourceTypeContainer) {
					if err := refuseCodeOnlyFlags(cmd, "this archive holds a container source"); err != nil {
						return err
					}
				}

				args, srcDir, containerDir = manifest.localSource(dir)
				id = manifestString(cmd, "id", id, manifest.ID)
				name = manifestString(cmd, appNameFlag, name, manifest.Name)
				baseImage = manifestString(cmd, "base-image", baseImage, manifest.Build.BaseImage)
				gpuType = manifestString(cmd, "gpu-type", gpuType, manifest.Compute.GpuType)
				maxWorkers = manifestInt32(cmd, "max-workers", maxWorkers, manifest.Compute.MaxWorkers)
				idleTTL = manifestInt32(cmd, "idle-ttl", idleTTL, manifest.Compute.IdleTTL)
				scalingDelay = manifestInt32(cmd, "scaling-delay", scalingDelay, manifest.Compute.ScalingDelay)
				if !cmd.Flags().Changed("requirement") {
					requirements = manifest.Build.Requirements
				}
				if !cmd.Flags().Changed("volume") {
					volumes = manifest.Volumes
				}
				// The map itself, not rendered as KEY=VALUE: a value that
				// legitimately carries quotes would be unquoted a second time
				// on the way back in.
				//
				// Passed whether or not --env-var was, because
				// buildEnvironmentVariables reads it before --env-file and
				// --env-var. One flag then overrides one key rather than
				// dropping the whole set a template needs to run.
				manifestEnv = manifest.EnvVars
				if !cmd.Flags().Changed("secret") {
					secrets = manifest.secretFlags()
				}
			}

			if id == "" {
				return fmt.Errorf("--id is required")
			}
			if err := validateGPUsPerWorkerFlag(cmd, gpusPerWorker); err != nil {
				return err
			}
			if cmd.Flags().Changed("available-workers-pct") {
				if err := validateAvailableWorkersPct(availableWorkersPct); err != nil {
					return err
				}
			}
			if cmd.Flags().Changed(appNameFlag) {
				if err := validateAppName(name); err != nil {
					return err
				}
			}
			if name == "" {
				name = id
			}

			archive, source, err := buildDeployArchive(srcDir, containerDir, baseImage, requirements, args)
			if err != nil {
				return err
			}

			client := newServerlessClient(cmd, logger)

			update, err := resolveDeployTarget(cmd, client, id, gpuType, from)
			if err != nil {
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
				appSecrets *[]serverlessapi.SecretAttach
			)
			if !update {
				appVolumes, err = buildVolumes(volumes)
				if err != nil {
					return err
				}
				appEnv, err = buildEnvironmentVariables(envFiles, envVars, manifestEnv)
				if err != nil {
					return err
				}
				appSecrets, err = parseSecretAttaches(secrets)
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
					Secrets:              appSecrets,
					Configuration: serverlessapi.WorkerConfigCreate{
						MaxWorkers:          maxWorkers,
						IdleTtlSecs:         idleTTL,
						ScalingDelaySecs:    scalingDelay,
						GpuType:             gpuType,
						MinWorkers:          manifestInt32Ptr(cmd, "min-workers", minWorkers, manifest.Compute.MinWorkers),
						GpusPerWorker:       manifestInt32Ptr(cmd, "gpus-per-worker", gpusPerWorker, manifest.Compute.GpusPerWorker),
						FallbackGpuType:     manifestStringPtr(cmd, "fallback-gpu-type", fallbackGPUType, manifest.Compute.FallbackGpuType),
						MinAvailableWorkers: manifestInt32Ptr(cmd, "min-available-workers", minAvailableWorkers, manifest.Compute.MinAvailableWorkers),
						AvailableWorkersPct: manifestInt32Ptr(cmd, "available-workers-pct", availableWorkersPct, manifest.Compute.AvailableWorkersPct),
					},
				})
			}
			if err != nil {
				spin.Stop()
				return err
			}
			appID := app.AppId
			waitCtx, cancelWait := waitContext(cmd.Context(), timeout)
			defer cancelWait()
			if wait && !serverlessapi.AppDeployTerminal(app.Status) {
				var lastStatus string
				app, err = waitForAppDeploy(waitCtx, client, appID, pollInterval, func(appStatus, buildStatus string) {
					msg := deployWaitMessage(appID, appStatus, buildStatus)
					if msg == lastStatus {
						return
					}
					lastStatus = msg
					_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s\n", msg)
					spin.SetMessage(msg)
				})
				if err != nil {
					spin.Stop()
					return waitTimeoutErr(waitCtx, err, "application "+appID, timeout)
				}
			}
			spin.Stop()

			// The endpoint rows are the outgoing version's until the submitted one
			// activates, and a source update on an already-active app answers
			// `active` throughout its build — so the wait above returns at once and
			// says nothing about whether this deploy landed. The pin is what moves
			// when it does.
			if canCompare {
				settled, err := waitForSubmittedVersion(waitCtx, client, app.AppId, versionBefore, pollInterval)
				if waitCtx.Err() == context.DeadlineExceeded {
					return waitTimeoutErr(waitCtx, err, "application "+app.AppId, timeout)
				}
				if err == nil && activationMoved(versionBefore, settled.ActiveVersionId) {
					if paths, err := deployEndpointPaths(cmd.Context(), client, app.AppId); err == nil {
						reportEndpointSetChange(cmd.ErrOrStderr(), compareEndpointSets(endpointsBefore, paths))
					}
				}
			} else if wait && !update && app.Status == serverlessapi.AppStatusActive {
				if paths, err := deployEndpointPaths(cmd.Context(), client, app.AppId); err == nil {
					reportDeployEndpoints(cmd.ErrOrStderr(), app, paths, defaultEnvironment(cmd))
				}
			}

			if err := output.Print(cmdutil.FormatFor(cmd), appResult(*app)); err != nil {
				return err
			}
			if !wait {
				return nil
			}
			return appFailedErr(cmd.Context(), client, app, defaultEnvironment(cmd))
		},
	}

	cmd.Flags().StringVar(&srcDir, "src-dir", "", "Directory to package as the application source (default: the working directory; code deploys only)")
	cmd.Flags().StringVar(&containerDir, "container", "", "Directory whose root contains Dockerfile and container.yaml")
	cmd.Flags().StringVar(&from, "from", "", "URL of a packaged source to fetch and deploy as a new application, reading its "+manifestFile)
	cmd.Flags().StringArrayVar(&volumes, "volume", nil, "Absolute path inside the app backed by persistent node-local storage; immutable after create (repeatable)")
	cmd.Flags().StringArrayVar(&secrets, "secret", nil, "Secret of the same environment to attach at create, as NAME or NAME=ENV_VAR (repeatable)")
	cmd.Flags().StringArrayVar(&envVars, envVarFlag, nil, "Environment variable as KEY=VALUE (repeatable)")
	cmd.Flags().StringArrayVar(&envFiles, "env-file", nil, "File of KEY=VALUE lines to read environment variables from (repeatable)")
	cmd.Flags().StringVar(&id, "id", "", "Application ID (immutable, lowercase slug)")
	cmd.Flags().StringVar(&name, appNameFlag, "", "Display name (defaults to --id)")
	cmd.Flags().Int32Var(&maxWorkers, "max-workers", 1, "Maximum number of workers")
	cmd.Flags().Int32Var(&idleTTL, "idle-ttl", 60, "Idle TTL in seconds before scaling down")
	cmd.Flags().Int32Var(&scalingDelay, "scaling-delay", 10, "Scaling delay in seconds")
	cmd.Flags().StringVar(&baseImage, "base-image", "python:3.12-slim", "Builder base image (code deploys only; needs Python 3.12 or newer)")
	cmd.Flags().StringVar(&gpuType, "gpu-type", "", "GPU type ID (see 'serverless gpus'; required when creating)")
	cmd.Flags().StringArrayVar(&requirements, "requirement", nil, "Additional pip package to install (repeatable; code deploys only)")
	cmd.Flags().Int32Var(&minWorkers, "min-workers", 0, "Minimum number of workers")
	cmd.Flags().Int32Var(&gpusPerWorker, "gpus-per-worker", 1, "GPUs allocated per worker ("+gpusPerWorkerValuesText()+")")
	cmd.Flags().StringVar(&fallbackGPUType, "fallback-gpu-type", "", "Secondary GPU type if the preferred type is unavailable")
	cmd.Flags().Int32Var(&minAvailableWorkers, "min-available-workers", 0, "Minimum idle workers kept as a buffer")
	cmd.Flags().Int32Var(&availableWorkersPct, "available-workers-pct", 0, "Idle-worker buffer as a percentage of load (0-100)")
	cmd.Flags().BoolVar(&wait, "wait", false, "Poll until the application is active or failed")
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "Maximum time to wait (0 = no limit)")
	cmd.Flags().DurationVar(&pollInterval, "poll-interval", 2*time.Second, "Polling interval when waiting for the application")
	addEnvironmentFlag(cmd, cmd.Flags(), logger, environmentFlagHelp)

	return cmd
}

// resolveDeployTarget reports whether the deploy updates an existing copy or
// creates one. Before the archive is uploaded it refuses a copy that is still
// deleting, create-only flags on an update, and an unknown --env or a missing
// --gpu-type on a create.
func resolveDeployTarget(cmd *cobra.Command, client *serverlessapi.Client, id, gpuType, from string) (update bool, err error) {
	app, err := existingApp(cmd.Context(), client, id)
	if err != nil {
		return false, err
	}
	if app != nil {
		if app.Status == serverlessapi.AppStatusDeleting {
			return false, fmt.Errorf("application %q is deleting; wait until it is deleted, then deploy", id)
		}
		if from != "" {
			return false, fmt.Errorf(
				"application %q already exists, and --from only creates: deploying would roll out this archive as the next version of that application, which keeps the secrets, environment variables and volumes it has now. Pass --id to create a separate application, or unpack the archive and deploy the directory to update that one",
				id,
			)
		}
		return true, validateUpdateDeployFlags(cmd, app)
	}
	if err := requireEnvironment(cmd.Context(), client, commandEnvironment(cmd)); err != nil {
		return false, err
	}
	return false, validateCreateDeployGPU(gpuType)
}

// existingApp returns the copy in the client's environment, or nil when getApp
// answers 404. The body is kept so a deleting copy can be refused before the
// upload; any other failure, including a 422 for a bad appId, is returned.
func existingApp(ctx context.Context, client *serverlessapi.Client, id string) (*serverlessapi.App, error) {
	app, err := client.GetApp(ctx, id)
	if err == nil {
		return app, nil
	}
	if isHTTPNotFound(err) {
		return nil, nil
	}
	return nil, err
}

// requireEnvironment fails when the named environment does not exist.
func requireEnvironment(ctx context.Context, client *serverlessapi.Client, environment string) error {
	if environment == "" {
		return nil
	}
	_, err := client.GetEnvironment(ctx, environment)
	if isHTTPNotFound(err) {
		return fmt.Errorf("environment %q not found", environment)
	}
	return err
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
	if slices.Contains(gpusPerWorkerAllowed, n) {
		return nil
	}
	return fmt.Errorf("--gpus-per-worker must be %s", gpusPerWorkerValuesText())
}

func validateGPUsPerWorkerFlag(cmd *cobra.Command, n int32) error {
	if !cmd.Flags().Changed("gpus-per-worker") {
		return nil
	}
	return validateGPUsPerWorker(n)
}

func validateAvailableWorkersPct(n int32) error {
	if n < 0 || n > 100 {
		return fmt.Errorf("--available-workers-pct must be between 0 and 100")
	}
	return nil
}

func validateUpdateDeployFlags(cmd *cobra.Command, app *serverlessapi.App) error {
	var names []string
	var hints []string
	seen := map[string]struct{}{}
	envChecked := false
	envSame := false
	var envErr error
	for _, name := range createOnlyDeployFlags {
		if !cmd.Flags().Changed(name) {
			continue
		}
		var (
			same bool
			err  error
		)
		if name == envVarFlag || name == "env-file" {
			if !envChecked {
				envSame, envErr = envFlagsMatchApp(cmd, app)
				envChecked = true
			}
			if envErr != nil {
				return envErr
			}
			same = envSame
		} else {
			same, err = createOnlyFlagMatches(cmd, app, name)
			if err != nil {
				return err
			}
		}
		if same {
			continue
		}
		names = append(names, "--"+name)
		hint := createOnlyDeployHint(name, commandEnvironment(cmd), defaultEnvironment(cmd))
		if _, ok := seen[hint]; !ok {
			seen[hint] = struct{}{}
			hints = append(hints, hint)
		}
	}
	if len(names) == 0 {
		return nil
	}
	verb := "applies"
	if len(names) != 1 {
		verb = "apply"
	}
	return fmt.Errorf("%s %s to create only; %s", strings.Join(names, ", "), verb, strings.Join(hints, "; "))
}

func createOnlyFlagMatches(cmd *cobra.Command, app *serverlessapi.App, name string) (bool, error) {
	cfg := app.Configuration
	switch name {
	case "gpu-type":
		got, err := cmd.Flags().GetString(name)
		return err == nil && sameOptionalString(got, cfg.GpuType), err
	case "fallback-gpu-type":
		got, err := cmd.Flags().GetString(name)
		return err == nil && sameOptionalString(got, cfg.FallbackGpuType), err
	case appNameFlag:
		got, err := cmd.Flags().GetString(name)
		return err == nil && got == app.AppName, err
	case "max-workers":
		return sameInt32Flag(cmd, name, cfg.MaxWorkers)
	case "idle-ttl":
		return sameInt32Flag(cmd, name, cfg.IdleTtlSecs)
	case "scaling-delay":
		return sameInt32Flag(cmd, name, cfg.ScalingDelaySecs)
	case "min-workers":
		return sameInt32Flag(cmd, name, cfg.MinWorkers)
	case "gpus-per-worker":
		return sameInt32Flag(cmd, name, cfg.GpusPerWorker)
	case "min-available-workers":
		return sameOptionalInt32Flag(cmd, name, cfg.MinAvailableWorkers)
	case "available-workers-pct":
		return sameOptionalInt32Flag(cmd, name, cfg.AvailableWorkersPct)
	case "volume":
		return false, nil
	case "secret":
		return secretsMatchApp(cmd, app)
	default:
		return false, nil
	}
}

func sameOptionalString[T ~string](got string, stored *T) bool {
	if stored == nil {
		return got == ""
	}
	return got == string(*stored)
}

func sameInt32Flag(cmd *cobra.Command, name string, stored int32) (bool, error) {
	got, err := cmd.Flags().GetInt32(name)
	if err != nil {
		return false, err
	}
	return got == stored, nil
}

func sameOptionalInt32Flag(cmd *cobra.Command, name string, stored *int32) (bool, error) {
	got, err := cmd.Flags().GetInt32(name)
	if err != nil {
		return false, err
	}
	if stored == nil {
		return got == 0, nil
	}
	return got == *stored, nil
}

func secretsMatchApp(cmd *cobra.Command, app *serverlessapi.App) (bool, error) {
	values, err := cmd.Flags().GetStringArray("secret")
	if err != nil {
		return false, err
	}
	parsed, err := parseSecretAttaches(values)
	if err != nil {
		return false, err
	}
	want := map[string]struct{}{}
	if parsed != nil {
		for _, item := range *parsed {
			want[secretMatchKey(item.SecretName, item.EnvVarName)] = struct{}{}
		}
	}
	have := map[string]struct{}{}
	for _, item := range app.Secrets {
		have[secretMatchKey(item.Name, item.EnvVarName)] = struct{}{}
	}
	return maps.Equal(want, have), nil
}

// secretMatchKey identifies one attachment. The API omits envVarName when it
// equals the secret name, and --secret NAME=NAME is that same attachment, so
// both sides treat that env name as empty.
func secretMatchKey(name string, env *string) string {
	got := ""
	if env != nil {
		got = *env
	}
	if got == name {
		got = ""
	}
	return name + "\x00" + got
}

func envFlagsMatchApp(cmd *cobra.Command, app *serverlessapi.App) (bool, error) {
	var files, pairs []string
	var err error
	if cmd.Flags().Changed("env-file") {
		files, err = cmd.Flags().GetStringArray("env-file")
		if err != nil {
			return false, err
		}
	}
	if cmd.Flags().Changed(envVarFlag) {
		pairs, err = cmd.Flags().GetStringArray(envVarFlag)
		if err != nil {
			return false, err
		}
	}
	built, err := buildEnvironmentVariables(files, pairs, nil)
	if err != nil {
		return false, err
	}
	stored := make(map[string]string, len(app.EnvironmentVariables))
	for _, item := range app.EnvironmentVariables {
		stored[item.Key] = item.Value
	}
	// A missing key is not the empty string. maps.Equal rejects an app that
	// holds only OLD=x against --env-var NEW=, which a lookup of the missing
	// key would accept.
	var got map[string]string
	if built != nil {
		got = *built
	}
	return maps.Equal(got, stored), nil
}

// createOnlyDeployHint says how to change a create-only setting on the copy
// in environment, under defaultEnvironment.
func createOnlyDeployHint(name, environment, defaultEnvironment string) string {
	switch name {
	case "gpu-type", "max-workers", "idle-ttl", "scaling-delay", "min-workers", "gpus-per-worker", "fallback-gpu-type", "min-available-workers", "available-workers-pct":
		return "use '" + serverlessCommand(environment, defaultEnvironment, "apps", "scale") + "' to change worker configuration"
	case envVarFlag, "env-file":
		return "use '" + serverlessCommand(environment, defaultEnvironment, "apps", envVarsGroup) + "' to change environment variables"
	case "secret":
		return "use '" + serverlessCommand(environment, defaultEnvironment, "secrets", "attach") + "' to attach a secret"
	case "volume":
		return "volumes are immutable after create"
	case appNameFlag:
		return "use '" + serverlessCommand(environment, defaultEnvironment, "apps", "rename") + "' to change the display name"
	default:
		return "omit it when updating an existing application"
	}
}

func parseSecretAttaches(values []string) (*[]serverlessapi.SecretAttach, error) {
	if len(values) == 0 {
		return nil, nil
	}
	out := make([]serverlessapi.SecretAttach, 0, len(values))
	for _, raw := range values {
		name, envVar, err := splitSecretAttach(raw)
		if err != nil {
			return nil, err
		}
		attach := serverlessapi.SecretAttach{
			SecretName: name,
		}
		if envVar != "" {
			attach.EnvVarName = &envVar
		}
		out = append(out, attach)
	}
	return &out, nil
}

func splitSecretAttach(raw string) (string, string, error) {
	name, envVar, hasEnv := strings.Cut(raw, "=")
	if name == "" || !envNamePattern.MatchString(name) {
		return "", "", fmt.Errorf("invalid --secret %q (want NAME or NAME=ENV_VAR)", raw)
	}
	if hasEnv && (envVar == "" || !envNamePattern.MatchString(envVar)) {
		return "", "", fmt.Errorf("invalid --secret %q (want NAME or NAME=ENV_VAR)", raw)
	}
	return name, envVar, nil
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
	page, err := client.ListBuilds(ctx, appID, serverlessapi.ListBuildsParams{})
	if err != nil || len(page.Data) == 0 {
		return ""
	}
	return string(page.Data[0].Status)
}

func appFailedErr(ctx context.Context, client *serverlessapi.Client, app *serverlessapi.App, defaultEnvironment string) error {
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
		return fmt.Errorf("application %s failed; inspect builds with '%s'", app.AppId, appCommand(app, defaultEnvironment, "apps", "builds", "list", app.AppId))
	default:
		return fmt.Errorf("application %s ended in status %s", app.AppId, app.Status)
	}
}

func latestBuildError(ctx context.Context, client *serverlessapi.Client, appID string) string {
	page, err := client.ListBuilds(ctx, appID, serverlessapi.ListBuildsParams{})
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
