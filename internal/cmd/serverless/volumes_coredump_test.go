//go:build linux || darwin

package serverless

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/runware/runware-cli/internal/coredump"
)

// coreDumpChildEnv marks the test process the test starts to run put in.
const coreDumpChildEnv = "RUNWARE_TEST_VOLUMES_PUT_CHILD"

// TestVolumesPutCannotDumpCoreBeforeReadingTheStatement runs put in a fresh
// process, which can dump core until put starts, and checks that the process
// could not when the fake answered the key-pair statement.
func TestVolumesPutCannotDumpCoreBeforeReadingTheStatement(t *testing.T) {
	if os.Getenv(coreDumpChildEnv) == "1" {
		if off, err := coredump.Disabled(); err != nil || off {
			t.Fatalf("core dumps were off before put ran (%v), so this proves nothing", err)
		}
		f := newPutFake(t)
		f.newVolume("", testVolume)
		mustPut(t, f.run(formatJSON, false, testVolume, newTree(t, testTree)))
		if len(f.coreDumpsOff) == 0 || slices.Contains(f.coreDumpsOff, false) {
			t.Fatalf("core dumps were allowed when the statement was read: %v", f.coreDumpsOff)
		}
		return
	}
	if off, _ := coredump.Disabled(); off {
		t.Skip("an earlier test in this process disabled core dumps, and a child process inherits that")
	}
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestVolumesPutCannotDumpCoreBeforeReadingTheStatement$", "-test.v") //nolint:gosec // the test binary itself
	cmd.Env = append(os.Environ(), coreDumpChildEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "--- PASS") {
		t.Fatalf("child: %v\n%s", err, out)
	}
}
