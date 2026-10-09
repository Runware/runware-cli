package public

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/runware/runware-cli/internal/installtest"
)

// installScript is the CLI-only install script served at
// cli.runware.ai/install.sh.
const installScript = "install.sh"

// installWithOverrides runs the install script against release with both
// overrides set. It returns the result and the install directory, which is
// not on PATH.
func installWithOverrides(t *testing.T, release *installtest.Release) (installtest.Result, string) {
	t.Helper()

	installDir := filepath.Join(t.TempDir(), "bin")
	res := installtest.RunSh(t, installtest.Invocation{
		Script: installScript,
		Env: map[string]string{
			"RUNWARE_INSTALL_DIR":      installDir,
			"RUNWARE_RELEASE_BASE_URL": release.URL,
		},
	})
	return res, installDir
}

// TestInstall_FreshInstallPutsAnExecutableCLIInTheInstallDirectory runs the
// CLI install script against a fake release and checks that the binary it
// served lands, executable, in RUNWARE_INSTALL_DIR.
func TestInstall_FreshInstallPutsAnExecutableCLIInTheInstallDirectory(t *testing.T) {
	t.Parallel()

	release := installtest.NewRelease(t, installtest.ReleaseConfig{})

	res, installDir := installWithOverrides(t, release)

	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0\n%s", res.ExitCode, res.Output)
	}
	info, err := os.Stat(filepath.Join(installDir, "runware"))
	if err != nil {
		t.Fatalf("installed CLI: %v\n%s", err, res.Output)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("installed CLI mode = %v, want executable", info.Mode())
	}
}

// TestInstall_ChecksumMismatchAbortsAndInstallsNothing serves an archive that
// does not match checksums.txt, as a tampered download would, and checks that
// the script refuses it.
func TestInstall_ChecksumMismatchAbortsAndInstallsNothing(t *testing.T) {
	t.Parallel()

	release := installtest.NewRelease(t, installtest.ReleaseConfig{
		BadChecksum: true,
	})

	res, installDir := installWithOverrides(t, release)

	if res.ExitCode == 0 {
		t.Fatalf("exit code = 0, want non-zero\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "Checksum mismatch") {
		t.Errorf("output does not report the checksum mismatch:\n%s", res.Output)
	}
	if _, err := os.Stat(filepath.Join(installDir, "runware")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("installed CLI after a checksum mismatch: stat error = %v, want not exist", err)
	}
}

// TestInstall_InstallDirectoryNotOnPathPrintsThePathFix installs into a
// directory that is not on PATH and checks that the script tells the user the
// line that puts it there.
func TestInstall_InstallDirectoryNotOnPathPrintsThePathFix(t *testing.T) {
	t.Parallel()

	release := installtest.NewRelease(t, installtest.ReleaseConfig{})

	res, installDir := installWithOverrides(t, release)

	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0\n%s", res.ExitCode, res.Output)
	}
	if !strings.Contains(res.Output, installDir+" is not in your PATH") {
		t.Errorf("output does not say %s is not on PATH:\n%s", installDir, res.Output)
	}
	fix := `export PATH="` + installDir + `:$PATH"`
	if !strings.Contains(res.Output, fix) {
		t.Errorf("output does not print the PATH fix %q:\n%s", fix, res.Output)
	}
}

// TestInstall_WithoutOverridesUsesGitHubAndTheHomeBinDirectory runs the script
// with neither override set. A curl shim keeps the run offline by sending the
// GitHub URLs to the fake release, and logs them, so the test can pin the URLs
// and install directory the script has always used.
func TestInstall_WithoutOverridesUsesGitHubAndTheHomeBinDirectory(t *testing.T) {
	t.Parallel()

	release := installtest.NewRelease(t, installtest.ReleaseConfig{})
	shim := release.GitHubShim(t)
	home := t.TempDir()

	res := installtest.RunSh(t, installtest.Invocation{
		Script: installScript,
		Env: map[string]string{
			"HOME": home,
		},
		PathDirs: []string{
			shim.Dir,
		},
	})

	if res.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0\n%s", res.ExitCode, res.Output)
	}
	archive := "runware_" + runtime.GOOS + "_" + runtime.GOARCH + ".tar.gz"
	download := "https://github.com/Runware/runware-cli/releases/download/" + installtest.DefaultTag + "/"
	want := []string{
		"https://api.github.com/repos/Runware/runware-cli/releases/latest",
		download + archive,
		download + "checksums.txt",
	}
	if got := shim.URLs(t); !slices.Equal(got, want) {
		t.Errorf("requested URLs:\n got %q\nwant %q", got, want)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "bin", "runware")); err != nil {
		t.Errorf("CLI not installed in ~/.local/bin: %v\n%s", err, res.Output)
	}
	if fix := `export PATH="$HOME/.local/bin:$PATH"`; !strings.Contains(res.Output, fix) {
		t.Errorf("output does not print the PATH fix %q:\n%s", fix, res.Output)
	}
}
