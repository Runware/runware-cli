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
	)

	cmd := &cobra.Command{
		Use:   "deploy <file>",
		Short: "Deploy a new serverless application",
		Long: `Create a new serverless application from a Python entry file.

The file is zipped and submitted as the application source. Worker settings
are supplied via flags (a local project config via 'runware serverless init'
is planned). Endpoints are derived server-side from the SDK.`,
		Example: `  # deploy a Python entry file
  runware serverless deploy ./app.py --id my-app --gpu-type h100

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

			zipBase64, modelFile, err := packPythonFile(entryFile)
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
				AppId:     id,
				AppName:   name,
				AppSource: source,
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
