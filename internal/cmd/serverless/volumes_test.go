package serverless

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/runware/runware-cli/internal/cmdutil"
	"github.com/runware/runware-cli/internal/output"
	"github.com/runware/runware-cli/internal/volumes/put"
	"gopkg.in/yaml.v3"
)

const (
	testVolume  = "model-weights"
	testVolume2 = "model-weights-copy"
)

var testTree = map[string]string{
	"config.json":       `{"layers": 4}`,
	"unet/weights.bin":  strings.Repeat("w", 70000),
	"vae/decoder.bin":   strings.Repeat("d", 1200),
	"tokenizer/vocab.t": "a b c",
}

func mustPut(t *testing.T, r putRun) put.Summary {
	t.Helper()
	if r.err != nil {
		t.Fatalf("put: %v\nstderr: %s", r.err, r.stderr)
	}
	var s put.Summary
	if err := json.Unmarshal([]byte(r.stdout), &s); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, r.stdout)
	}
	return s
}

func actions(s put.Summary) map[string]string {
	out := make(map[string]string, len(s.Files))
	for _, f := range s.Files {
		out[f.Path] = f.Action
	}
	return out
}

func requireOneLine(t *testing.T, err error, wants ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("put succeeded, want an error naming %q", wants)
	}
	msg := err.Error()
	if strings.Contains(msg, "\n") {
		t.Errorf("error is more than one line: %q", msg)
	}
	for _, want := range wants {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not name %q", msg, want)
		}
	}
}

func hasCall(calls []string, prefix, contains string) bool {
	for _, c := range calls {
		if strings.HasPrefix(c, prefix) && strings.Contains(c, contains) {
			return true
		}
	}
	return false
}

func TestVolumesPutIsRegisteredInTheHiddenVolumesGroup(t *testing.T) {
	root := NewCmd(log.New(io.Discard))
	put, _, err := root.Find([]string{segVolumes, putUse})
	if err != nil || put.Name() != "put" {
		t.Fatalf("volumes put is not registered: %v", err)
	}
	if !put.Parent().Hidden {
		t.Error("the volumes group is not hidden")
	}
	for _, flag := range []string{toFlag, environmentFlag} {
		if put.Flags().Lookup(flag) == nil {
			t.Errorf("volumes put has no --%s", flag)
		}
	}
	if put.Flags().ShorthandLookup(environmentShorthand) == nil {
		t.Errorf("volumes put has no -%s", environmentShorthand)
	}
}

func TestVolumesPutHelpDescribesAdditiveUploads(t *testing.T) {
	root := NewCmd(log.New(io.Discard))
	cmd, _, err := root.Find([]string{segVolumes, putUse})
	if err != nil {
		t.Fatal(err)
	}
	help := strings.ToLower(cmd.Long)
	for _, want := range []string{"additive", "workers that start after an upload get the new files", "rolling an app back", "create a new volume"} {
		if !strings.Contains(help, want) {
			t.Errorf("help does not say %q", want)
		}
	}
	for _, promise := range []string{"delete files", "download", "version history"} {
		if strings.Contains(help, promise) {
			t.Errorf("help promises %q", promise)
		}
	}
	if cmd.Example == "" {
		t.Error("help has no example")
	}
}

func TestVolumesPutUploadsADirectoryAndARepeatChangesNothing(t *testing.T) {
	f := newPutFake(t)
	f.newVolume("", testVolume)
	dir := newTree(t, testTree)

	first := mustPut(t, f.run(formatJSON, false, testVolume, dir))
	files, digest := f.volume("", testVolume)
	if got, want := slices.Sorted(maps.Keys(files)), slices.Sorted(maps.Keys(testTree)); !slices.Equal(got, want) {
		t.Fatalf("Volume files = %v, want %v", got, want)
	}
	if !first.Changed || first.Added != len(testTree) || first.ManifestSHA256 != digest || first.BytesSent == 0 {
		t.Fatalf("first summary = %+v, digest %s", first, digest)
	}

	again := mustPut(t, f.run(formatJSON, false, testVolume, dir))
	_, after := f.volume("", testVolume)
	if after != digest || again.ManifestSHA256 != digest {
		t.Fatalf("digest moved from %s to %s (summary %s)", digest, after, again.ManifestSHA256)
	}
	if again.Changed || again.BytesSent != 0 || again.Reused != len(testTree) || again.Added+again.Replaced != 0 {
		t.Fatalf("repeat summary = %+v, want no change and 0 bytes", again)
	}
}

func TestVolumesPutReportsAddedReplacedAndReused(t *testing.T) {
	f := newPutFake(t)
	f.newVolume("", testVolume)
	f.newVolume("", testVolume2)
	dir := newTree(t, testTree)
	mustPut(t, f.run(formatJSON, false, testVolume, dir))

	writeTree(t, dir, map[string]string{"extra/notes.txt": "new"})
	added := mustPut(t, f.run(formatJSON, false, testVolume, dir))
	if added.Added != 1 || actions(added)["extra/notes.txt"] != put.ActionAdded || added.Replaced != 0 {
		t.Fatalf("adding one file = %+v", added)
	}
	files, _ := f.volume("", testVolume)
	if len(files) != len(testTree)+1 {
		t.Fatalf("Volume lists %d files, want %d", len(files), len(testTree)+1)
	}

	writeTree(t, dir, map[string]string{"config.json": `{"layers": 8}`})
	replaced := mustPut(t, f.run(formatJSON, false, testVolume, dir))
	if replaced.Replaced != 1 || actions(replaced)["config.json"] != put.ActionReplaced || replaced.Added != 0 {
		t.Fatalf("changing one file = %+v", replaced)
	}

	reused := mustPut(t, f.run(formatJSON, false, testVolume2, dir))
	// Every path is new to the second Volume, and all of its content is reused.
	if reused.Added != len(testTree)+1 || reused.Reused != len(testTree)+1 || reused.BytesSent != 0 || reused.Replaced != 0 {
		t.Fatalf("second Volume = %+v, want every file added with reused content and 0 bytes", reused)
	}
}

func TestVolumesPutPlacesFilesUnderTo(t *testing.T) {
	f := newPutFake(t)
	f.newVolume("", testVolume)
	dir := newTree(t, map[string]string{
		"a.txt":   "a",
		"sub/b.t": "b",
	})
	mustPut(t, f.run(formatJSON, false, testVolume, dir, "--to", "models/base"))
	files, _ := f.volume("", testVolume)
	if got := slices.Sorted(maps.Keys(files)); !slices.Equal(got, []string{"models/base/a.txt", "models/base/sub/b.t"}) {
		t.Fatalf("with --to, files = %v", got)
	}

	f.newVolume("", testVolume2)
	mustPut(t, f.run(formatJSON, false, testVolume2, dir))
	files, _ = f.volume("", testVolume2)
	if got := slices.Sorted(maps.Keys(files)); !slices.Equal(got, []string{"a.txt", "sub/b.t"}) {
		t.Fatalf("without --to, files = %v", got)
	}
}

func TestVolumesPutTargetsTheEnvironment(t *testing.T) {
	cases := []struct {
		name    string
		envVar  string
		args    []string
		wantEnv string
	}{
		{
			name:    "flag",
			args:    []string{"-e", testEnvironment},
			wantEnv: testEnvironment,
		},
		{
			name:    "RUNWARE_ENV",
			envVar:  "dev",
			wantEnv: "dev",
		},
		{
			name:    "flag over RUNWARE_ENV",
			envVar:  "dev",
			args:    []string{"--env", testEnvironment},
			wantEnv: testEnvironment,
		},
		{
			name: testProductionEnvironment,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPutFake(t)
			t.Setenv("RUNWARE_ENV", tc.envVar)
			f.newVolume(tc.wantEnv, testVolume)
			dir := newTree(t, map[string]string{"a.txt": "a"})
			s := mustPut(t, f.run(formatJSON, false, append([]string{testVolume, dir}, tc.args...)...))
			wantName := tc.wantEnv
			if wantName == "" {
				wantName = testProductionEnvironment
			}
			if files, _ := f.volume(tc.wantEnv, testVolume); len(files) != 1 {
				t.Errorf("the Volume in %q holds %d files, want 1", tc.wantEnv, len(files))
			}
			if s.Environment != wantName {
				t.Errorf("summary environment = %q, want %q", s.Environment, wantName)
			}
			for _, call := range f.callLog() {
				if strings.Contains(call, "volumes-key-pair") {
					if strings.Contains(call, "environment=") {
						t.Errorf("the key-pair read names an environment: %s", call)
					}
					continue
				}
				if hasEnv := strings.Contains(call, "?environment="+tc.wantEnv); tc.wantEnv != "" && !hasEnv {
					t.Errorf("call %s does not address %s", call, tc.wantEnv)
				}
				if tc.wantEnv == "" && strings.Contains(call, "environment=") {
					t.Errorf("a production call names an environment: %s", call)
				}
			}
		})
	}
}

func TestVolumesPutSummaryIsTheSameInEveryFormat(t *testing.T) {
	// The same change in each format, each against its own organization, so
	// no run reuses content another one stored.
	dir := newTree(t, testTree)
	formats := map[string]put.Summary{}
	for _, format := range []string{formatJSON, formatYAML, "table"} {
		f := newPutFake(t)
		f.newVolume("", testVolume)
		f.pageSize = 2
		mustPut(t, f.run(formatJSON, false, testVolume, filepath.Join(dir, "unet")))
		r := f.run(format, false, testVolume, dir)
		if r.err != nil {
			t.Fatalf("%s: %v", format, r.err)
		}
		var s put.Summary
		switch format {
		case formatJSON:
			if err := json.Unmarshal([]byte(r.stdout), &s); err != nil {
				t.Fatalf("json stdout is not one object: %v", err)
			}
		case formatYAML:
			var raw map[string]any
			if err := yaml.Unmarshal([]byte(r.stdout), &raw); err != nil {
				t.Fatalf("yaml stdout: %v", err)
			}
			b, err := json.Marshal(raw)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(b, &s); err != nil {
				t.Fatal(err)
			}
		default:
			s = parseTextSummary(t, r.stdout)
		}
		files, digest := f.volume("", testVolume)
		if s.ManifestSHA256 == "" || s.ManifestSHA256 != digest {
			t.Errorf("%s digest = %q, Volume reports %s", format, s.ManifestSHA256, digest)
		}
		for _, fr := range s.Files {
			vf, ok := files[fr.Path]
			if !ok || vf.Size != fr.Size || vf.SHA256 != fr.SHA256 {
				t.Errorf("%s reports %+v, Volume lists %+v", format, fr, vf)
			}
		}
		formats[format] = s
	}
	j := formats[formatJSON]
	// Four new paths, one of them with content the first put stored.
	if j.Added != 4 || j.Reused != 1 || j.BytesSent == 0 {
		t.Errorf("json summary = %+v, want 4 added and 1 reused", j)
	}
	for _, format := range []string{formatYAML, "table"} {
		s := formats[format]
		if s.ManifestSHA256 != j.ManifestSHA256 || s.Added != j.Added || s.Replaced != j.Replaced || s.Reused != j.Reused || s.BytesSent != j.BytesSent || !slices.Equal(s.Files, j.Files) {
			t.Errorf("%s summary %+v differs from json %+v", format, s, j)
		}
	}
}

// parseTextSummary reads the text summary back into a Summary.
func parseTextSummary(t *testing.T, text string) put.Summary {
	t.Helper()
	var s put.Summary
	for line := range strings.SplitSeq(text, "\n") {
		fields := strings.Fields(line)
		switch {
		case strings.HasPrefix(strings.TrimSpace(line), "Latest files digest:"):
			s.ManifestSHA256 = fields[len(fields)-1]
		case strings.HasPrefix(strings.TrimSpace(line), "Files:"):
			mustScan(t, line, "  Files: %d added, %d replaced, %d reused", &s.Added, &s.Replaced, &s.Reused)
		case strings.HasPrefix(strings.TrimSpace(line), "Bytes sent:"):
			mustScan(t, fields[2], "%d", &s.BytesSent)
		case len(fields) == 8 && fields[5] == "sent" && fields[6] == "sha256":
			var size, sent int64
			mustScan(t, fields[2], "%d", &size)
			mustScan(t, fields[4], "%d", &sent)
			s.Files = append(s.Files, put.FileResult{
				Action:    fields[0],
				Path:      fields[1],
				Size:      size,
				SHA256:    fields[7],
				BytesSent: sent,
			})
		}
	}
	return s
}

func TestVolumesPutWritesProgressToStandardError(t *testing.T) {
	f := newPutFake(t)
	f.newVolume("", testVolume)
	r := f.run(formatJSON, false, testVolume, newTree(t, testTree))
	mustPut(t, r)
	if !strings.Contains(r.stderr, "files") || strings.Contains(r.stdout, "files,") {
		t.Fatalf("progress belongs on stderr only: stderr %q stdout %q", r.stderr, r.stdout)
	}
}

func TestVolumesPutStopsWhenAFileChangesDuringTheUpload(t *testing.T) {
	f := newPutFake(t)
	f.newVolume("", testVolume)
	dir := newTree(t, testTree)
	mustPut(t, f.run(formatJSON, false, testVolume, dir))
	_, digest := f.volume("", testVolume)
	before, _ := f.volume("", testVolume)

	changed := filepath.Join(dir, "unet", "weights.bin")
	writeTree(t, dir, map[string]string{"unet/weights.bin": strings.Repeat("x", 70000)})
	f.onDeclare = func() {
		// After put hashed the file and before it finishes uploading it.
		future := time.Now().Add(time.Hour)
		if err := os.WriteFile(changed, []byte(strings.Repeat("y", 70001)), 0o600); err != nil {
			t.Error(err)
		}
		_ = os.Chtimes(changed, future, future)
	}
	r := f.run(formatJSON, false, testVolume, dir)
	requireOneLine(t, r.err, "weights.bin", "changed")
	after, afterDigest := f.volume("", testVolume)
	if afterDigest != digest || !maps.Equal(before, after) {
		t.Fatal("a failed put changed the Volume")
	}

	f.onDeclare = nil
	again := mustPut(t, f.run(formatJSON, false, testVolume, dir))
	if actions(again)["unet/weights.bin"] != put.ActionReplaced || again.Replaced != 1 {
		t.Fatalf("rerun = %+v, want the changed file replaced", again)
	}
}

func TestVolumesPutWithoutAnOrganizationSaysWhereToSetIt(t *testing.T) {
	f := newPutFake(t)
	f.newVolume("", testVolume)
	t.Setenv("RUNWARE_ORGANIZATION", "")
	r := f.run(formatJSON, false, testVolume, newTree(t, testTree))
	requireOneLine(t, r.err, "RUNWARE_ORGANIZATION", "config.yaml", "console")
	if calls := f.callLog(); len(calls) != 0 {
		t.Fatalf("put sent %v without an organization", calls)
	}
}

func TestVolumesPutRefusesAStatementBeforeDeclaring(t *testing.T) {
	cases := map[string]func(t *testing.T, f *putFake){
		"another organization": func(t *testing.T, _ *putFake) {
			t.Setenv("RUNWARE_ORGANIZATION", "8d1f6c1e-2c64-4c9e-9a51-6c0f3a1d7b2e")
		},
		"expired statement": func(_ *testing.T, f *putFake) { f.notAfter = time.Now().Add(-time.Minute) },
		"invalid signature": func(_ *testing.T, f *putFake) { f.badSignature = true },
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := newPutFake(t)
			f.newVolume("", testVolume)
			setup(t, f)
			r := f.run(formatJSON, false, testVolume, newTree(t, testTree))
			requireOneLine(t, r.err, "key-pair statement refused")
			calls := f.callLog()
			if hasCall(calls, "POST", "/uploads") {
				t.Fatalf("put opened or declared after a refused statement: %v", calls)
			}
		})
	}
}

// TestVolumesPutBlamesTheAPIKeyOnlyForAnAPIRefusal: a 401 from the Volumes API
// prints the CLI's hint about the API key, and a 401 for one part (a refused
// part token) does not.
func TestVolumesPutBlamesTheAPIKeyOnlyForAnAPIRefusal(t *testing.T) {
	t.Run("API key refused", func(t *testing.T) {
		f := newPutFake(t)
		f.newVolume("", testVolume)
		t.Setenv("RUNWARE_API_KEY", "another-key")
		r := f.run(formatJSON, false, testVolume, newTree(t, testTree))
		if r.err == nil {
			t.Fatal("put succeeded with the API key refused")
		}
		if printed := printedError(t, r.err); !strings.Contains(printed, "Authentication failed") || !strings.Contains(printed, "runware auth status") {
			t.Fatalf("printed %q, want the hint about the API key", printed)
		}
	})
	t.Run("part token refused", func(t *testing.T) {
		f := newPutFake(t)
		f.newVolume("", testVolume)
		f.refusePart = true
		f.partRefusalStatus = http.StatusUnauthorized
		r := f.run(formatJSON, false, testVolume, newTree(t, testTree))
		if r.err == nil {
			t.Fatal("put succeeded with every part refused")
		}
		if msg := r.err.Error(); strings.Contains(msg, "API key") || !strings.Contains(msg, "part token was refused") {
			t.Fatalf("error = %q, want the part refusal and no word about the API key", msg)
		}
		if printed := printedError(t, r.err); strings.Contains(printed, "Authentication failed") || strings.Contains(printed, "auth status") {
			t.Fatalf("printed %q, want no word about the API key", printed)
		}
	})
}

// TestVolumesPutPrintsAnAPIRefusalWithItsFields: a Volumes API refusal prints
// like any other serverless error, with the API's code and problem type, and
// keeps the step that failed in its message.
func TestVolumesPutPrintsAnAPIRefusalWithItsFields(t *testing.T) {
	f := newPutFake(t)
	f.newVolume("", testVolume)
	f.refuseOpenStatus = http.StatusForbidden
	r := f.run(formatJSON, false, testVolume, newTree(t, testTree))
	if r.err == nil {
		t.Fatal("put succeeded with the session refused")
	}
	var structured bytes.Buffer
	cmdutil.PrintErrorTo(log.New(io.Discard), &structured, output.FormatJSON, r.err)
	var printed struct {
		Errors []map[string]any `json:"errors"`
	}
	if err := json.Unmarshal(structured.Bytes(), &printed); err != nil || len(printed.Errors) != 1 {
		t.Fatalf("printed %s, want one error (%v)", structured.String(), err)
	}
	got := printed.Errors[0]
	if got["code"] != "forbidden" || got["problemType"] != fakeProblemBase+"refused" {
		t.Errorf("printed fields %v, want the API's code and problem type", got)
	}
	if msg, _ := got["message"].(string); !strings.Contains(msg, "open the upload") || !strings.Contains(msg, "the session was refused") {
		t.Errorf("printed message %q, want the step and the API's detail", msg)
	}
	if text := printedError(t, r.err); !strings.Contains(text, "code=forbidden") {
		t.Errorf("printed %q, want the API's code", text)
	}
}

func TestVolumesPutRefusalsAreOneActionableLine(t *testing.T) {
	cases := []struct {
		name  string
		setup func(f *putFake, dir string)
		args  []string
		wants []string
		// avoid is text the error must not hold.
		avoid []string
	}{
		{
			name:  "Volume not found or not enabled",
			args:  []string{"no-such-volume"},
			wants: []string{"no-such-volume", "not found", "not enabled", testEnvFlag},
		},
		{
			name:  "Volume gone by the declaration",
			setup: func(f *putFake, _ string) { f.missingOnDeclare = true },
			wants: []string{testVolume, "not found", "not enabled"},
		},
		{
			name:  "file unknown at its completion",
			setup: func(f *putFake, _ string) { f.missingOnComplete = true },
			wants: []string{"complete the file", "file not found"},
			avoid: []string{"not enabled"},
		},
		{
			name:  "file over maxFileBytes",
			setup: func(f *putFake, _ string) { f.maxFileBytes = 10000 },
			wants: []string{"weights.bin", "maxFileBytes", "10000"},
		},
		{
			name: "Volume over maxFilesPerVolume",
			setup: func(f *putFake, _ string) {
				f.limitOnDeclare = &fakeLimit{
					name: "maxFilesPerVolume",
					cap:  3,
				}
			},
			wants: []string{"maxFilesPerVolume", "3"},
		},
		{
			name: "new content over maxStoredBytes",
			setup: func(f *putFake, _ string) {
				f.limitOnDeclare = &fakeLimit{
					name: "maxStoredBytes",
					cap:  5000,
				}
			},
			wants: []string{"maxStoredBytes", "5000"},
		},
		{
			name: "a file in the Volume is a directory in the upload",
			setup: func(f *putFake, _ string) {
				f.volumes["/"+testVolume].files["unet"] = put.VolumeFile{
					Path:   "unet",
					Mode:   0o444,
					Size:   1,
					SHA256: sha256Hex("u"),
				}
			},
			wants: []string{"weights.bin", "a file and a directory"},
		},
		{
			name:  "upload session no longer open",
			setup: func(f *putFake, _ string) { f.closeOnDeclare = true },
			wants: []string{"no longer open", "run put again"},
		},
		{
			name:  "upload session closed before a file completes",
			setup: func(f *putFake, _ string) { f.closeOnComplete = true },
			wants: []string{"no longer open", "run put again"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newPutFake(t)
			f.newVolume("", testVolume)
			dir := newTree(t, testTree)
			if tc.setup != nil {
				tc.setup(f, dir)
			}
			args := tc.args
			if args == nil {
				args = []string{testVolume}
			}
			r := f.run(formatJSON, false, append(args, dir)...)
			requireOneLine(t, r.err, tc.wants...)
			for _, avoid := range tc.avoid {
				if r.err != nil && strings.Contains(r.err.Error(), avoid) {
					t.Errorf("error %q holds %q", r.err, avoid)
				}
			}
		})
	}
}

func TestVolumesPutPrintsNoSecret(t *testing.T) {
	cases := map[string]func(f *putFake){
		"successful upload":  func(*putFake) {},
		"refused part":       func(f *putFake) { f.refusePart = true },
		"refused completion": func(f *putFake) { f.refuseComplete = true },
	}
	for name, setup := range cases {
		for _, verbose := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s verbose=%t", name, verbose), func(t *testing.T) {
				f := newPutFake(t)
				f.newVolume("", testVolume)
				setup(f)
				out := f.run("table", verbose, testVolume, newTree(t, testTree)).output()
				for _, marker := range []string{markerToken, markerAck, markerURL} {
					if strings.Contains(out, marker) {
						t.Errorf("output shows %s:\n%s", marker, out)
					}
				}
				for _, secret := range f.sentSecrets() {
					if secret != "" && strings.Contains(out, secret) {
						t.Errorf("output shows a sealed key or encapsulation:\n%s", out)
					}
				}
				if verbose && !strings.Contains(out, "serverless response") {
					t.Error("the verbose run logged no response, so it checked nothing")
				}
				if verbose && !strings.Contains(out, "volume part upload") {
					t.Error("the verbose run logged no part upload, so it checked nothing")
				}
			})
		}
	}
}

// TestVolumesPutStopsWhenTheKeySetupFails: a put whose statement keys cannot be
// set up fails with that error before any request reaches the Volumes API.
func TestVolumesPutStopsWhenTheKeySetupFails(t *testing.T) {
	f := newPutFake(t)
	f.newVolume("", testVolume)
	newVolumeKeys = func(string, string) (put.Keys, error) {
		return nil, errors.New("the statement keys could not be set up")
	}
	r := f.run(formatJSON, false, testVolume, newTree(t, testTree))
	if r.err == nil || !strings.Contains(r.err.Error(), "the statement keys could not be set up") {
		t.Fatalf("err = %v, want the key setup error", r.err)
	}
	if calls := f.callLog(); len(calls) != 0 {
		t.Fatalf("requests reached the Volumes API: %v", calls)
	}
}

// TestNewVerifiedVolumeKeysPassesAVerifierErrorOn: newVerifiedVolumeKeys returns
// the error NewVerifier gives, here for a missing state directory.
func TestNewVerifiedVolumeKeysPassesAVerifierErrorOn(t *testing.T) {
	keys, err := newVerifiedVolumeKeys("https://api.example.com", "")
	if err == nil || keys != nil {
		t.Fatalf("keys = %v, err = %v, want the verifier's error", keys, err)
	}
}

func TestVolumesPutKeepsItsStateInTheConfigVolumesDirectory(t *testing.T) {
	f := newPutFake(t)
	f.newVolume("", testVolume)
	mustPut(t, f.run(formatJSON, false, testVolume, newTree(t, testTree)))
	state := filepath.Join(f.home, ".runware", volumesStateDir)
	if _, err := os.Stat(filepath.Join(state, "hashes.json")); err != nil {
		t.Errorf("hashes.json is not in %s: %v", state, err)
	}
	// The statement verifier keeps its sequence record in the directory it is given.
	if dirs := f.keysDirs; len(dirs) != 1 || dirs[0] != state {
		t.Errorf("the statement keys were given %v, want [%s]", dirs, state)
	}
	// The keys are chosen for the API host the uploads go to.
	if urls := f.keysBaseURLs; len(urls) != 1 || urls[0] != f.api.URL {
		t.Errorf("the statement keys were chosen for %v, want [%s]", urls, f.api.URL)
	}
	// The part transport's upload host check is made for the same API host.
	if urls := f.transportBaseURLs; len(urls) != 1 || urls[0] != f.api.URL {
		t.Errorf("the part transport was made for %v, want [%s]", urls, f.api.URL)
	}

	// The journal of an upload in progress is there too.
	var journals []string
	f.onDeclare = func() {
		entries, _ := os.ReadDir(filepath.Join(state, "uploads"))
		for _, e := range entries {
			journals = append(journals, e.Name())
		}
	}
	mustPut(t, f.run(formatJSON, false, testVolume, newTree(t, map[string]string{"other.bin": "o"})))
	if len(journals) == 0 {
		t.Fatalf("no upload journal under %s while the upload ran", filepath.Join(state, "uploads"))
	}
}

func mustScan(t *testing.T, s, format string, args ...any) {
	t.Helper()
	if _, err := fmt.Sscanf(s, format, args...); err != nil {
		t.Fatalf("scan %q as %q: %v", s, format, err)
	}
}
