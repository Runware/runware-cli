package serverless

import (
	"encoding/json"
	"strings"
	"testing"

	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestWorkerConfigPatchFromFlags_EachFlag(t *testing.T) {
	cases := []struct {
		args []string
		key  string
		want any
	}{
		{[]string{"--max-workers", "2"}, "maxWorkers", float64(2)},
		{[]string{"--min-workers", "0"}, "minWorkers", float64(0)},
		{[]string{"--idle-ttl", "120"}, "idleTtlSecs", float64(120)},
		{[]string{"--scaling-delay", "15"}, "scalingDelaySecs", float64(15)},
		{[]string{"--concurrency", "4"}, "concurrency", float64(4)},
		{[]string{"--gpu-type", testGPUType}, "gpuType", testGPUType},
		{[]string{"--gpus-per-worker", "2"}, "gpusPerWorker", float64(2)},
		{[]string{"--fallback-gpu-type", testGPUType}, "fallbackGpuType", testGPUType},
		{[]string{"--min-available-workers", "1"}, "minAvailableWorkers", float64(1)},
		{[]string{"--available-workers-pct", "50"}, "availableWorkersPct", float64(50)},
	}

	registered, _ := newScaleFlagCmd()
	tested := make(map[string]struct{}, len(cases))
	for _, tc := range cases {
		name := strings.TrimPrefix(tc.args[0], "--")
		tested[name] = struct{}{}

		cmd, flags := newScaleFlagCmd()
		if err := cmd.ParseFlags(tc.args); err != nil {
			t.Fatalf("%s: ParseFlags: %v", name, err)
		}

		patch, err := workerConfigPatchFromFlags(cmd, *flags)
		if err != nil {
			t.Fatalf("%s: workerConfigPatchFromFlags: %v", name, err)
		}

		raw, err := json.Marshal(patch)
		if err != nil {
			t.Fatalf("%s: marshal patch: %v", name, err)
		}
		var got map[string]any
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("%s: unmarshal patch: %v", name, err)
		}
		if len(got) != 1 {
			t.Fatalf("%s: omitted flags should not appear, got %s", name, raw)
		}
		if got[tc.key] != tc.want {
			t.Fatalf("%s: JSON %s=%v, want %v (%s)", name, tc.key, got[tc.key], tc.want, raw)
		}

		wrap, err := json.Marshal(serverlessapi.DeploymentUpdate{Configuration: patch})
		if err != nil {
			t.Fatalf("%s: marshal update: %v", name, err)
		}
		var outer map[string]json.RawMessage
		if err := json.Unmarshal(wrap, &outer); err != nil {
			t.Fatalf("%s: unmarshal update: %v", name, err)
		}
		if len(outer) != 1 {
			t.Fatalf("%s: update body should only include configuration, got %s", name, wrap)
		}
		if _, ok := outer["configuration"]; !ok {
			t.Fatalf("%s: missing configuration: %s", name, wrap)
		}
	}

	registered.LocalFlags().VisitAll(func(f *pflag.Flag) {
		if _, ok := tested[f.Name]; !ok {
			t.Errorf("flag %q is registered but has no patch test", f.Name)
		}
	})
	for name := range tested {
		if registered.LocalFlags().Lookup(name) == nil {
			t.Errorf("patch test has %q but bindScaleFlags did not register it", name)
		}
	}
}

func TestWorkerConfigPatchFromFlags_RequiresAFlag(t *testing.T) {
	cmd, flags := newScaleFlagCmd()
	if err := cmd.ParseFlags([]string{}); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	_, err := workerConfigPatchFromFlags(cmd, *flags)
	if err == nil {
		t.Fatal("expected error when no scaling flags are set")
	}
	if !strings.Contains(err.Error(), "at least one scaling flag") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func newScaleFlagCmd() (*cobra.Command, *scaleFlags) {
	cmd := &cobra.Command{Use: "scale"}
	flags := &scaleFlags{}
	bindScaleFlags(cmd, flags)
	return cmd, flags
}
