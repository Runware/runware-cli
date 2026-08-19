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
	"github.com/spf13/pflag"
)

// scaleFlags are worker-config values bound to apps scale flags.
type scaleFlags struct {
	maxWorkers          int32
	minWorkers          int32
	idleTTL             int32
	scalingDelay        int32
	concurrency         int32
	gpuType             string
	gpusPerWorker       int32
	fallbackGPUType     string
	minAvailableWorkers int32
	availableWorkersPct int32
}

func newAppsScaleCmd(logger *log.Logger) *cobra.Command {
	var flags scaleFlags

	cmd := &cobra.Command{
		Use:   "scale <appId>",
		Short: "Scale a serverless application",
		Long: `Patch live worker configuration for a serverless application.

Omitted flags are left unchanged. Configuration changes take effect on the
next scaler cycle; this command does not wait for a rollout.

The server rejects unsupported or invalid fields with HTTP 422.`,
		Example: `  # set the worker cap
  runware serverless apps scale my-app --max-workers 2

  # scale to zero and raise idle TTL
  runware serverless apps scale my-app --min-workers 0 --idle-ttl 120

  # change GPU type (applies to newly created workers)
  runware serverless apps scale my-app --gpu-type h100`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			patch, err := workerConfigPatchFromFlags(cmd, flags)
			if err != nil {
				return err
			}

			spin := cmdutil.NewSpinner(fmt.Sprintf("Updating application %s...", id))
			spin.Start()

			client := serverlessapi.NewClient(config.GetAPIKey(), config.GetServerlessBaseURL(), slog.New(logger))
			app, err := client.UpdateApp(cmd.Context(), id, serverlessapi.AppUpdate{
				Configuration: patch,
			})
			if err != nil {
				spin.Stop()
				return err
			}
			spin.Stop()

			return output.Print(cmdutil.FormatFor(cmd), appResult(*app))
		},
	}

	bindScaleFlags(cmd, &flags)
	var names []string
	cmd.LocalFlags().VisitAll(func(f *pflag.Flag) {
		names = append(names, f.Name)
	})
	cmd.MarkFlagsOneRequired(names...)
	return cmd
}

func bindScaleFlags(cmd *cobra.Command, flags *scaleFlags) {
	f := cmd.Flags()
	f.Int32Var(&flags.maxWorkers, "max-workers", 0, "Maximum number of workers")
	f.Int32Var(&flags.minWorkers, "min-workers", 0, "Minimum number of workers (0 = scale to zero)")
	f.Int32Var(&flags.idleTTL, "idle-ttl", 0, "Idle TTL in seconds before scaling down")
	f.Int32Var(&flags.scalingDelay, "scaling-delay", 0, "Scaling delay in seconds")
	f.Int32Var(&flags.concurrency, "concurrency", 0, "Max tasks a single worker handles simultaneously")
	f.StringVar(&flags.gpuType, "gpu-type", "", "Preferred GPU type ID (see 'serverless gpus')")
	f.Int32Var(&flags.gpusPerWorker, "gpus-per-worker", 0, "GPUs allocated per worker")
	f.StringVar(&flags.fallbackGPUType, "fallback-gpu-type", "", "Secondary GPU type if the preferred type is unavailable")
	f.Int32Var(&flags.minAvailableWorkers, "min-available-workers", 0, "Minimum idle workers kept as a buffer")
	f.Int32Var(&flags.availableWorkersPct, "available-workers-pct", 0, "Idle-worker buffer as a percentage of load (0-100)")
}

// workerConfigPatchFromFlags builds a partial configuration from flags that
// were explicitly set. Unchanged flags are omitted so existing values are
// not cleared.
func workerConfigPatchFromFlags(cmd *cobra.Command, flags scaleFlags) (*serverlessapi.WorkerConfigPatch, error) {
	patch := &serverlessapi.WorkerConfigPatch{
		MaxWorkers:          optionalInt32Ptr(cmd, "max-workers", flags.maxWorkers),
		MinWorkers:          optionalInt32Ptr(cmd, "min-workers", flags.minWorkers),
		IdleTtlSecs:         optionalInt32Ptr(cmd, "idle-ttl", flags.idleTTL),
		ScalingDelaySecs:    optionalInt32Ptr(cmd, "scaling-delay", flags.scalingDelay),
		Concurrency:         optionalInt32Ptr(cmd, "concurrency", flags.concurrency),
		GpuType:             optionalFlagStringPtr(cmd, "gpu-type", flags.gpuType),
		GpusPerWorker:       optionalInt32Ptr(cmd, "gpus-per-worker", flags.gpusPerWorker),
		FallbackGpuType:     optionalFlagStringPtr(cmd, "fallback-gpu-type", flags.fallbackGPUType),
		MinAvailableWorkers: optionalInt32Ptr(cmd, "min-available-workers", flags.minAvailableWorkers),
		AvailableWorkersPct: optionalInt32Ptr(cmd, "available-workers-pct", flags.availableWorkersPct),
	}
	if *patch == (serverlessapi.WorkerConfigPatch{}) {
		return nil, fmt.Errorf("at least one scaling flag is required")
	}
	return patch, nil
}
