package serverless

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/runware/runware-cli/internal/installtest"
)

// forbiddenTools are the tools ADR 0001 keeps out of the install scripts:
// init installs the Serverless library, so a script never runs pip, uv or
// python itself.
var forbiddenTools = []string{
	"pip",
	"pip3",
	"uv",
	"python",
	"python3",
}

// pathStubName is the name of a stub CLI that a test puts on PATH before the
// script runs, as a CLI the user installed earlier.
const pathStubName = "path"

// installer is one of the Serverless install scripts and the way a test runs
// it.
type installer struct {
	name   string
	script string
	run    func(testing.TB, installtest.Invocation) installtest.Result
	// appDir passes the App directory to the script. An empty dir passes
	// nothing, so the script uses its default.
	appDir func(inv *installtest.Invocation, dir string)
}

var installers = []installer{
	{
		name:   "sh",
		script: "install.sh",
		run:    installtest.RunSh,
		appDir: func(inv *installtest.Invocation, dir string) {
			if dir != "" {
				inv.Args = []string{
					dir,
				}
			}
		},
	},
	{
		name:   "pwsh",
		script: "install.ps1",
		run:    installtest.RunPwsh,
		appDir: func(inv *installtest.Invocation, dir string) {
			if dir != "" {
				inv.Env["RUNWARE_APP_DIR"] = dir
			}
		},
	},
}

// scenario is the state a test sets up before it runs an installer.
type scenario struct {
	release installtest.ReleaseConfig
	// appDir is the App directory to pass. Empty passes none.
	appDir string
	// pathStub, when not nil, puts a stub CLI named pathStubName on PATH.
	pathStub *installtest.StubConfig
}

// outcome is what a test observes after an installer ran.
type outcome struct {
	installtest.Result

	release    *installtest.Release
	installDir string
	calls      []installtest.StubCall
}

// installed reports whether the script left a CLI in the install directory.
func (o outcome) installed() bool {
	_, err := os.Stat(filepath.Join(o.installDir, "runware"))
	return err == nil
}

// callsBy returns the argv of each call that the named stub received.
func (o outcome) callsBy(stub string) [][]string {
	var args [][]string
	for _, c := range o.calls {
		if c.Stub == stub {
			args = append(args, c.Args)
		}
	}
	return args
}

// runScenario runs inst against a fake release, with RUNWARE_INSTALL_DIR set
// to a directory that is not on PATH and tripwires for forbiddenTools first
// on PATH. It fails the test when a tripwire ran.
func runScenario(t *testing.T, inst installer, sc scenario) outcome {
	t.Helper()

	release := installtest.NewRelease(t, sc.release)
	tw := installtest.NewTripwires(t, forbiddenTools...)
	installDir := filepath.Join(t.TempDir(), "bin")
	inv := installtest.Invocation{
		Script: inst.script,
		Env: map[string]string{
			"RUNWARE_INSTALL_DIR":      installDir,
			"RUNWARE_RELEASE_BASE_URL": release.URL,
		},
		PathDirs: []string{
			tw.Dir,
		},
	}
	if sc.pathStub != nil {
		inv.PathDirs = append(inv.PathDirs, installtest.WriteStub(t, *sc.pathStub))
	}
	inst.appDir(&inv, sc.appDir)

	res := inst.run(t, inv)

	if calls := tw.Calls(t); len(calls) > 0 {
		t.Errorf("%s ran tools that ADR 0001 forbids: %q\n%s", inst.script, calls, res.Output)
	}
	return outcome{
		Result:     res,
		release:    release,
		installDir: installDir,
		calls:      res.StubCalls(t),
	}
}

// assertLastInit fails the test unless the last call that stub received was
// init with dir.
func (o outcome) assertLastInit(t *testing.T, stub, dir string) {
	t.Helper()

	calls := o.callsBy(stub)
	if want := installtest.InitArgs(dir); len(calls) == 0 || !slices.Equal(calls[len(calls)-1], want) {
		t.Errorf("calls to stub %q = %q, want the last to be %q\n%s", stub, calls, want, o.Output)
	}
}

// eachInstaller runs test once per installer, as parallel subtests.
func eachInstaller(t *testing.T, test func(t *testing.T, inst installer)) {
	t.Helper()

	for _, inst := range installers {
		t.Run(inst.name, func(t *testing.T) {
			t.Parallel()
			test(t, inst)
		})
	}
}

// TestInstall_FreshInstallRunsInitWithTheGivenDirectory installs the CLI
// from a fake release onto a machine that has none, and checks that the
// script then runs the installed CLI's init with the App directory it was
// given.
func TestInstall_FreshInstallRunsInitWithTheGivenDirectory(t *testing.T) {
	t.Parallel()

	eachInstaller(t, func(t *testing.T, inst installer) {
		o := runScenario(t, inst, scenario{
			appDir: "my app",
		})

		if o.ExitCode != 0 {
			t.Fatalf("exit code = %d, want 0\n%s", o.ExitCode, o.Output)
		}
		if !o.installed() {
			t.Errorf("no CLI in %s\n%s", o.installDir, o.Output)
		}
		o.assertLastInit(t, installtest.ReleaseStubName, "my app")
	})
}

// TestInstall_NoDirectoryArgumentInitsMyApp runs the script with no App
// directory and checks that init gets the default, my-app.
func TestInstall_NoDirectoryArgumentInitsMyApp(t *testing.T) {
	t.Parallel()

	eachInstaller(t, func(t *testing.T, inst installer) {
		o := runScenario(t, inst, scenario{})

		if o.ExitCode != 0 {
			t.Fatalf("exit code = %d, want 0\n%s", o.ExitCode, o.Output)
		}
		o.assertLastInit(t, installtest.ReleaseStubName, "my-app")
	})
}

// TestInstall_CLIWithInitOnPathIsReused puts a CLI that has init on PATH and
// checks that the script runs init with it and downloads nothing.
func TestInstall_CLIWithInitOnPathIsReused(t *testing.T) {
	t.Parallel()

	eachInstaller(t, func(t *testing.T, inst installer) {
		o := runScenario(t, inst, scenario{
			appDir: "my-app",
			pathStub: &installtest.StubConfig{
				Name: pathStubName,
			},
		})

		if o.ExitCode != 0 {
			t.Fatalf("exit code = %d, want 0\n%s", o.ExitCode, o.Output)
		}
		if got := o.release.Requests(); len(got) > 0 {
			t.Errorf("release requests = %q, want none", got)
		}
		if o.installed() {
			t.Errorf("installed a CLI in %s, want the one on PATH reused", o.installDir)
		}
		o.assertLastInit(t, pathStubName, "my-app")
	})
}

// TestInstall_CLIWithoutInitOnPathIsReplacedByAFreshInstall puts a CLI from
// before init on PATH. That CLI answers the init probe with exit 0, as a real
// one does. The script must still call it too old, install the latest
// release and run init with the new CLI.
func TestInstall_CLIWithoutInitOnPathIsReplacedByAFreshInstall(t *testing.T) {
	t.Parallel()

	eachInstaller(t, func(t *testing.T, inst installer) {
		o := runScenario(t, inst, scenario{
			appDir: "my-app",
			pathStub: &installtest.StubConfig{
				Name:        pathStubName,
				WithoutInit: true,
			},
		})

		if o.ExitCode != 0 {
			t.Fatalf("exit code = %d, want 0\n%s", o.ExitCode, o.Output)
		}
		if !strings.Contains(o.Output, "too old") {
			t.Errorf("output does not say the CLI on PATH is too old:\n%s", o.Output)
		}
		if !o.installed() {
			t.Errorf("no CLI in %s\n%s", o.installDir, o.Output)
		}
		for _, call := range o.callsBy(pathStubName) {
			if slices.Equal(call, installtest.InitArgs("my-app")) {
				t.Errorf("ran init with the CLI on PATH, which has no init")
			}
		}
		o.assertLastInit(t, installtest.ReleaseStubName, "my-app")
	})
}

// TestInstall_ChecksumMismatchAbortsBeforeAnythingRuns serves an archive that
// does not match checksums.txt and checks that the script stops before it
// installs or runs the CLI.
func TestInstall_ChecksumMismatchAbortsBeforeAnythingRuns(t *testing.T) {
	t.Parallel()

	eachInstaller(t, func(t *testing.T, inst installer) {
		o := runScenario(t, inst, scenario{
			release: installtest.ReleaseConfig{
				BadChecksum: true,
			},
		})

		if o.ExitCode == 0 {
			t.Fatalf("exit code = 0, want non-zero\n%s", o.Output)
		}
		if !strings.Contains(o.Output, "Checksum mismatch") {
			t.Errorf("output does not report the checksum mismatch:\n%s", o.Output)
		}
		if o.installed() {
			t.Errorf("installed a CLI in %s after a checksum mismatch", o.installDir)
		}
		if len(o.calls) > 0 {
			t.Errorf("stub calls = %q, want none", o.calls)
		}
	})
}

// TestInstall_InstallDirectoryNotOnPathRunsInitThenPrintsThePathFix installs
// into a directory that is not on PATH. The script must still reach the new
// CLI by its absolute path, and print the PATH fix after init.
func TestInstall_InstallDirectoryNotOnPathRunsInitThenPrintsThePathFix(t *testing.T) {
	t.Parallel()

	eachInstaller(t, func(t *testing.T, inst installer) {
		o := runScenario(t, inst, scenario{
			appDir: "my-app",
		})

		if o.ExitCode != 0 {
			t.Fatalf("exit code = %d, want 0\n%s", o.ExitCode, o.Output)
		}
		initAt := strings.Index(o.Output, "stub serverless init my-app")
		if initAt < 0 {
			t.Fatalf("init did not run:\n%s", o.Output)
		}
		fix := `export PATH="` + o.installDir + `:$PATH"`
		fixAt := strings.Index(o.Output, fix)
		if fixAt < 0 {
			t.Fatalf("output does not print the PATH fix %q:\n%s", fix, o.Output)
		}
		if fixAt < initAt {
			t.Errorf("PATH fix printed before init ran:\n%s", o.Output)
		}
		if !strings.Contains(o.Output, o.installDir+" is not in your PATH") {
			t.Errorf("output does not say %s is not on PATH:\n%s", o.installDir, o.Output)
		}
	})
}

// TestInstall_InitFailureFailsTheScript serves a CLI whose init exits 3 and
// checks that the script exits non-zero too.
func TestInstall_InitFailureFailsTheScript(t *testing.T) {
	t.Parallel()

	eachInstaller(t, func(t *testing.T, inst installer) {
		o := runScenario(t, inst, scenario{
			release: installtest.ReleaseConfig{
				Stub: installtest.Stub(installtest.StubConfig{
					Name:     installtest.ReleaseStubName,
					InitExit: 3,
				}),
			},
		})

		if o.ExitCode == 0 {
			t.Errorf("exit code = 0, want non-zero after init failed\n%s", o.Output)
		}
		if !strings.Contains(o.Output, "stub serverless init my-app") {
			t.Errorf("init did not run:\n%s", o.Output)
		}
	})
}

// TestInstall_LatestReleaseWithoutInitFailsBeforeInit serves a latest release
// from before init. Its CLI would print help for 'serverless init my-app' and
// exit 0, so the script must refuse it instead of reporting success.
func TestInstall_LatestReleaseWithoutInitFailsBeforeInit(t *testing.T) {
	t.Parallel()

	eachInstaller(t, func(t *testing.T, inst installer) {
		o := runScenario(t, inst, scenario{
			release: installtest.ReleaseConfig{
				Stub: installtest.Stub(installtest.StubConfig{
					Name:        installtest.ReleaseStubName,
					WithoutInit: true,
				}),
			},
		})

		if o.ExitCode == 0 {
			t.Errorf("exit code = 0, want non-zero for a CLI without init\n%s", o.Output)
		}
		for _, call := range o.calls {
			if slices.Equal(call.Args, installtest.InitArgs("my-app")) {
				t.Errorf("ran init with a CLI that has no init\n%s", o.Output)
			}
		}
	})
}
