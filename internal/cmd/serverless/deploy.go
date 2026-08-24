package serverless

import (
	"fmt"
	"log/slog"

	"github.com/charmbracelet/log"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/spf13/cobra"
)

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
		volumes       []string
		envVars       []string
		envFiles      []string
	)

	cmd := &cobra.Command{
		Use:   "deploy <file>",
		Short: "Deploy a new serverless application",
		Long: `Create a new serverless application from a Python entry file.

The whole source directory is zipped and submitted as the application source, so
the entry file can import its own modules and read its own data files. That
directory is the working directory unless --src-dir says otherwise.

The entry file must live inside the source directory. A relative path is resolved
inside it; an absolute path is taken as given.

Exclude what the app does not need with a .runwareignore file at the root of the
source directory; it takes gitignore syntax. A .gitignore is NOT consulted --
what a project keeps out of version control is a different question from what it
ships. Either way .env files are never uploaded, and neither are .git,
__pycache__, .venv, node_modules or the usual build and tool caches.

Environment variables must be supplied here with --env or --env-file. An app's
environment is frozen into the version this command creates, which is what the
worker is rendered from, so setting one afterwards with 'apps env set' stores it
without it ever reaching a pod. Prefer --env-file for anything secret: a value
passed as --env is visible in the process list and recorded in shell history.

Anything the app downloads at runtime belongs on a --volume. The app runs in a
sandbox whose filesystem is part of the checkpointed state, so an unmounted
download is copied into every checkpoint and fetched again on every cold start.
A volume keeps it out of both.

Worker settings are supplied via flags (a local project config via 'runware
serverless init' is planned). Endpoints are derived server-side from the SDK.`,
		Example: `  # deploy the current directory, with app.py as the entry point
  runware serverless deploy ./app.py --id my-app --gpu-type h100

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
    --base-image python:3.11-slim --requirement torch`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			entryFile := args[0]
			if name == "" {
				name = id
			}

			zipBase64, modelFile, err := packDirectory(srcDir, entryFile)
			if err != nil {
				return err
			}

			appVolumes, err := buildVolumes(volumes)
			if err != nil {
				return err
			}

			appEnv, err := buildEnvironmentVariables(envFiles, envVars)
			if err != nil {
				return err
			}

			source, err := serverlessapi.NewCodeAppSource(serverlessapi.CodeSourceUpsert{
				BaseImage: baseImage,
				Codebase: serverlessapi.CodebaseSource{
					ModelFile: modelFile,
					ZipBase64: zipBase64,
				},
				Requirements: optionalStringSlice(requirements),
			})
			if err != nil {
				return fmt.Errorf("build application source: %w", err)
			}

			body := serverlessapi.AppCreate{
				AppId:                id,
				AppName:              name,
				AppSource:            source,
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
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Creating application %s...", id))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			app, err := client.CreateApp(cmd.Context(), body)
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return output.Print(cmdutil.FormatFor(cmd), appResult(*app))
		},
	}

	cmd.Flags().StringVar(&srcDir, "src-dir", "", "Directory to package as the application source (default: the working directory)")
	cmd.Flags().StringArrayVar(&volumes, "volume", nil, "Absolute path inside the app backed by persistent node-local storage (repeatable)")
	cmd.Flags().StringArrayVar(&envVars, "env", nil, "Environment variable as KEY=VALUE (repeatable)")
	cmd.Flags().StringArrayVar(&envFiles, "env-file", nil, "File of KEY=VALUE lines to read environment variables from (repeatable)")
	cmd.Flags().StringVar(&id, "id", "", "Application ID (immutable, lowercase slug)")
	cmd.Flags().StringVar(&name, "name", "", "Display name (defaults to --id)")
	cmd.Flags().Int32Var(&maxWorkers, "max-workers", 1, "Maximum number of workers")
	cmd.Flags().Int32Var(&idleTTL, "idle-ttl", 60, "Idle TTL in seconds before scaling down")
	cmd.Flags().Int32Var(&scalingDelay, "scaling-delay", 10, "Scaling delay in seconds")
	cmd.Flags().StringVar(&baseImage, "base-image", "python:3.11-slim", "Builder base image")
	cmd.Flags().StringVar(&gpuType, "gpu-type", "", "GPU type ID (see 'serverless gpus')")
	cmd.Flags().StringArrayVar(&requirements, "requirement", nil, "Additional pip package to install (repeatable)")
	cmd.Flags().Int32Var(&minWorkers, "min-workers", 0, "Minimum number of workers")
	cmd.Flags().Int32Var(&gpusPerWorker, "gpus-per-worker", 1, "GPUs allocated per worker")

	if err := cmd.MarkFlagRequired("id"); err != nil {
		panic(err)
	}
	if err := cmd.MarkFlagRequired("gpu-type"); err != nil {
		panic(err)
	}

	return cmd
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
