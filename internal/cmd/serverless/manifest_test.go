package serverless

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

const testManifestID = "resumable-job"

const codeManifest = `configVersion: 1
id: resumable-job
source:
  type: code
  entry: model.py
compute:
  gpuType: rtx-pro-6000
secrets:
  - name: S3_BUCKET
  - name: HF_TOKEN
    envVarName: HUGGING_FACE_HUB_TOKEN
`

// writeManifest puts body at the root of a new directory and returns it.
func writeManifest(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, manifestFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func readManifestBody(t *testing.T, body string) deployManifest {
	t.Helper()
	manifest, err := readDeployManifest(writeManifest(t, body))
	if err != nil {
		t.Fatalf("readDeployManifest: %v", err)
	}
	return manifest
}

func manifestError(t *testing.T, body string) string {
	t.Helper()
	_, err := readDeployManifest(writeManifest(t, body))
	if err == nil {
		t.Fatal("expected an error")
	}
	return err.Error()
}

func TestReadDeployManifestCode(t *testing.T) {
	manifest := readManifestBody(t, codeManifest)

	if manifest.ID != testManifestID {
		t.Errorf("id = %q", manifest.ID)
	}
	if manifest.Compute.GpuType != "rtx-pro-6000" {
		t.Errorf("compute.gpuType = %q", manifest.Compute.GpuType)
	}

	args, srcDir, containerDir := manifest.localSource("/unpacked")
	if len(args) != 1 || args[0] != testEntryFile {
		t.Errorf("args = %v", args)
	}
	if srcDir != "/unpacked" || containerDir != "" {
		t.Errorf("srcDir = %q, containerDir = %q", srcDir, containerDir)
	}

	secrets := manifest.secretFlags()
	want := []string{"S3_BUCKET", "HF_TOKEN=HUGGING_FACE_HUB_TOKEN"}
	if strings.Join(secrets, ",") != strings.Join(want, ",") {
		t.Errorf("secretFlags = %v, want %v", secrets, want)
	}
}

func TestReadDeployManifestContainer(t *testing.T) {
	manifest := readManifestBody(t, `configVersion: 1
id: render-farm
source:
  type: container
compute:
  gpuType: rtx-pro-6000
  maxWorkers: 4
`)

	args, srcDir, containerDir := manifest.localSource("/unpacked")
	if args != nil || srcDir != "" {
		t.Errorf("a container app named args %v and srcDir %q", args, srcDir)
	}
	if containerDir != "/unpacked" {
		t.Errorf("containerDir = %q", containerDir)
	}
	if manifest.Compute.MaxWorkers == nil || *manifest.Compute.MaxWorkers != 4 {
		t.Errorf("compute.maxWorkers = %v", manifest.Compute.MaxWorkers)
	}
}

// envVarFlags has to be ordered: an unordered map would make two deploys of
// one manifest differ in the order they report and send variables.
func TestManifestEnvVarFlagsAreSorted(t *testing.T) {
	manifest := readManifestBody(t, `configVersion: 1
id: config-and-secrets
source:
  type: code
  entry: model.py
envVars:
  LOG_LEVEL: info
  CACHE_DIR: /weights
  BATCH: "8"
`)

	got := strings.Join(manifest.envVarFlags(), " ")
	want := "BATCH=8 CACHE_DIR=/weights LOG_LEVEL=info"
	if got != want {
		t.Errorf("envVarFlags = %q, want %q", got, want)
	}
}

// A misspelled key is the failure this file exists to catch: ignored, it turns
// into a missing required flag that says nothing about the typo behind it.
func TestReadDeployManifestRejectsUnknownKey(t *testing.T) {
	msg := manifestError(t, `configVersion: 1
id: echo-app
source:
  type: code
  entry: model.py
compute:
  gpu_type: rtx-pro-6000
`)
	if !strings.Contains(msg, "gpu_type") {
		t.Errorf("error does not name the unknown key: %s", msg)
	}
}

func TestReadDeployManifestVersion(t *testing.T) {
	missing := manifestError(t, `id: echo-app
source:
  type: code
  entry: model.py
`)
	if !strings.Contains(missing, "configVersion is required") {
		t.Errorf("missing configVersion: %s", missing)
	}

	future := manifestError(t, `configVersion: 2
id: echo-app
source:
  type: code
  entry: model.py
`)
	if !strings.Contains(future, "not supported") {
		t.Errorf("future configVersion: %s", future)
	}
}

func TestReadDeployManifestRequiresSource(t *testing.T) {
	for body, want := range map[string]string{
		"configVersion: 1\nid: echo-app\n":                                            "source.type is required",
		"configVersion: 1\nid: echo-app\nsource:\n  type: wasm\n":                     "is not code or container",
		"configVersion: 1\nid: echo-app\nsource:\n  type: code\n":                     "source.entry is required",
		"configVersion: 1\nsource:\n  type: code\n  entry: model.py\n":                "id is required",
		"configVersion: 1\nid: echo-app\nsource:\n  type: container\n  entry: m.py\n": "source.entry applies to a code app only",
	} {
		if msg := manifestError(t, body); !strings.Contains(msg, want) {
			t.Errorf("%q: got %q, want it to mention %q", body, msg, want)
		}
	}
}

// The entry file is resolved inside a directory this command created, so a
// path that climbs out of it would read the machine running the deploy.
func TestReadDeployManifestRejectsEscapingEntry(t *testing.T) {
	for _, entry := range []string{"../../etc/passwd", "/etc/passwd"} {
		msg := manifestError(t, "configVersion: 1\nid: echo-app\nsource:\n  type: code\n  entry: "+entry+"\n")
		if !strings.Contains(msg, "source.entry") {
			t.Errorf("entry %q: %s", entry, msg)
		}
	}
}

func TestReadDeployManifestMissingFile(t *testing.T) {
	_, err := readDeployManifest(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "nothing to deploy it with") {
		t.Fatalf("err = %v", err)
	}
}

func TestReadDeployManifestRejectsBadSecret(t *testing.T) {
	msg := manifestError(t, `configVersion: 1
id: echo-app
source:
  type: code
  entry: model.py
secrets:
  - name: not a name
`)
	if !strings.Contains(msg, "secret") {
		t.Errorf("error = %s", msg)
	}
}

func TestReadDeployManifestRejectsBadCompute(t *testing.T) {
	msg := manifestError(t, `configVersion: 1
id: echo-app
source:
  type: code
  entry: model.py
compute:
  gpusPerWorker: 3
`)
	if !strings.Contains(msg, "gpusPerWorker") {
		t.Errorf("error = %s", msg)
	}
}

// mergeCmd is a command carrying the deploy flags the merge helpers consult.
func mergeCmd(t *testing.T, passed ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{
		Use: "deploy",
		RunE: func(*cobra.Command, []string) error {
			return nil
		},
	}
	cmd.Flags().String("id", "", "")
	cmd.Flags().String("gpu-type", "", "")
	cmd.Flags().Int32("max-workers", 1, "")
	cmd.Flags().Int32("min-workers", 0, "")
	cmd.Flags().String("fallback-gpu-type", "", "")

	cmd.SetArgs(passed)
	cmd.SetOut(nil)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	return cmd
}

// A flag the person passed wins over the manifest, which is what lets one
// archive deploy on another GPU type without being edited.
func TestManifestMergePrefersPassedFlags(t *testing.T) {
	cmd := mergeCmd(t, "--gpu-type", testFallbackGPU, "--min-workers", "2")

	if got := manifestString(cmd, "gpu-type", testFallbackGPU, "rtx-pro-6000"); got != testFallbackGPU {
		t.Errorf("gpu-type = %q, want the passed l40s", got)
	}
	if got := manifestString(cmd, "id", "", testManifestID); got != testManifestID {
		t.Errorf("id = %q, want the manifest's", got)
	}

	fromManifest := int32(4)
	if got := manifestInt32(cmd, "max-workers", 1, &fromManifest); got != 4 {
		t.Errorf("max-workers = %d, want the manifest's 4", got)
	}

	manifestMin := int32(1)
	if got := manifestInt32Ptr(cmd, "min-workers", 2, &manifestMin); got == nil || *got != 2 {
		t.Errorf("min-workers = %v, want the passed 2", got)
	}
}

// Nothing in the merge may invent a value. An absent field has to leave the
// API request without the key, so the server's own default applies.
func TestManifestMergeKeepsAbsentAbsent(t *testing.T) {
	cmd := mergeCmd(t)

	if got := manifestInt32Ptr(cmd, "min-workers", 0, nil); got != nil {
		t.Errorf("min-workers = %v, want nil", got)
	}
	if got := manifestStringPtr(cmd, "fallback-gpu-type", "", ""); got != nil {
		t.Errorf("fallback-gpu-type = %v, want nil", got)
	}
	if got := manifestStringPtr(cmd, "fallback-gpu-type", "", testFallbackGPU); got == nil || *got != testFallbackGPU {
		t.Errorf("fallback-gpu-type = %v, want the manifest's l40s", got)
	}
	if got := manifestInt32(cmd, "max-workers", 1, nil); got != 1 {
		t.Errorf("max-workers = %d, want the flag default 1", got)
	}
}

// With --from the id comes out of a file the person deploying did not write,
// so an id the platform cannot accept has to be named here rather than come
// back as a path-parameter complaint.
func TestReadDeployManifestRejectsBadAppID(t *testing.T) {
	for _, id := range []string{"echo", "Echo-App", "-echo-app", "echo-app-", "echo_app", strings.Repeat("e", 31)} {
		msg := manifestError(t, "configVersion: 1\nid: "+id+"\nsource:\n  type: code\n  entry: model.py\n")
		if !strings.Contains(msg, "valid application id") {
			t.Errorf("id %q: %s", id, msg)
		}
	}
	for _, id := range []string{"echo-app", testManifestID, "a1b2c3", strings.Repeat("e", 30)} {
		if _, err := readDeployManifest(writeManifest(t, "configVersion: 1\nid: "+id+"\nsource:\n  type: code\n  entry: model.py\n")); err != nil {
			t.Errorf("id %q was refused: %v", id, err)
		}
	}
}
