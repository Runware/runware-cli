package put

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/runware/runware-cli/internal/config"
	"github.com/runware/runware-cli/internal/output"
	"github.com/runware/runware-cli/internal/volumes/keypair"
)

const testServerlessURL = "https://api.example.com"

// standardTree makes the directory most tests upload.
func standardTree(t *testing.T, e *testEnv) string {
	t.Helper()
	dir := filepath.Join(e.srcDir, "weights")
	writeTree(t, dir, map[string][]byte{
		"config.json":           patterned(13, 1),
		"empty.txt":             {},
		"model/weights.bin":     patterned(3<<20, 2),
		"model/nested/deep.bin": patterned(1049560, 3),
	})
	return dir
}

func filesByPath(sum Summary) map[string]FileResult {
	out := make(map[string]FileResult, len(sum.Files))
	for _, f := range sum.Files {
		out[f.Path] = f
	}
	return out
}

func requireFiles(t *testing.T, e *testEnv, vol VolumeRef, want []VolumeFile) string {
	t.Helper()
	got, digest := e.cloud.latest(vol)
	if !slices.Equal(got, want) {
		t.Fatalf("the Volume's latest files are\n%+v\nwant\n%+v", got, want)
	}
	return digest
}

// AC 1: a directory put twice leaves the digest unchanged and sends nothing the second time.
func TestPutNewVolumeThenRepeat(t *testing.T) {
	e := newTestEnv(t)
	dir := standardTree(t, e)
	vol := e.cloud.newVolume("model-weights", "")
	want := expectedFiles(t, dir, "")

	first := e.mustPut(vol, dir)
	digest := requireFiles(t, e, vol, want)
	if !first.Changed || first.ManifestSHA256 != digest || digest == "" {
		t.Errorf("first put: changed = %v, digest = %q, the Volume's digest is %q", first.Changed, first.ManifestSHA256, digest)
	}
	if first.Added != len(want) || first.Replaced != 0 || first.Reused != 0 {
		t.Errorf("first put: %d added, %d replaced, %d reused, want %d added", first.Added, first.Replaced, first.Reused, len(want))
	}
	if first.BytesSent != totalCiphertext(t, want) {
		t.Errorf("first put sent %d bytes, want the files' ciphertext, %d", first.BytesSent, totalCiphertext(t, want))
	}
	if dirs := e.uploadDirs(); len(dirs) != 0 {
		t.Errorf("resume state left after a completed put: %v", dirs)
	}
	if !e.keys.allCleared() {
		t.Error("a session or file key was not cleared after the put")
	}
	hashed, puts := e.hashedCount(), len(e.cloud.putLog())
	if hashed != len(want) {
		t.Errorf("first put hashed %d files, want %d", hashed, len(want))
	}

	second := e.mustPut(vol, dir)
	if second.Changed {
		t.Error("an unchanged repeat reports a change")
	}
	if second.ManifestSHA256 != digest || requireFiles(t, e, vol, want) != digest {
		t.Errorf("the repeat moved the digest: %q, want %q", second.ManifestSHA256, digest)
	}
	if second.BytesSent != 0 || second.Reused != len(want) || second.Added != 0 || second.Replaced != 0 {
		t.Errorf("repeat: %d bytes, %d reused, %d added, %d replaced, want zero bytes and %d reused", second.BytesSent, second.Reused, second.Added, second.Replaced, len(want))
	}
	if got := len(e.cloud.putLog()); got != puts {
		t.Errorf("the repeat sent %d parts", got-puts)
	}
	if got := e.hashedCount(); got != hashed {
		t.Errorf("the repeat hashed %d files again", got-hashed)
	}
	if !e.keys.allCleared() {
		t.Error("the data key of a reused file was not cleared")
	}
}

func TestPutUnderDestinationDirectory(t *testing.T) {
	e := newTestEnv(t)
	dir := standardTree(t, e)
	vol := e.cloud.newVolume("model-weights", "")
	opts := e.options(vol, dir)
	opts.To = "models/v1"

	sum, err := Run(t.Context(), opts)

	if err != nil {
		t.Fatal(err)
	}
	requireFiles(t, e, vol, expectedFiles(t, dir, "models/v1"))
	for _, f := range sum.Files {
		if !strings.HasPrefix(f.Path, "models/v1/") {
			t.Errorf("summary path %s is not under the destination", f.Path)
		}
	}
}

func TestPutAddressesTheEnvironment(t *testing.T) {
	e := newTestEnv(t)
	dir := standardTree(t, e)
	staging := e.cloud.newVolume("model-weights", "staging")

	sum := e.mustPut(staging, dir)

	requireFiles(t, e, staging, expectedFiles(t, dir, ""))
	if sum.Environment != "staging" {
		t.Errorf("environment = %q, want staging", sum.Environment)
	}
	if _, err := e.put(VolumeRef{Name: "model-weights"}, dir); err == nil {
		t.Error("a put without the environment found the staging Volume")
	}
}

// AC 2: one new file uploads alone.
func TestPutAddsOneFile(t *testing.T) {
	e := newTestEnv(t)
	dir := standardTree(t, e)
	vol := e.cloud.newVolume("model-weights", "")
	e.mustPut(vol, dir)
	puts := len(e.cloud.putLog())

	extra := filepath.Join(dir, "extra.bin")
	writeFile(t, extra, patterned(2<<20, 4))
	sum := e.mustPut(vol, extra)

	if sum.Added != 1 || sum.Replaced != 0 || sum.Reused != 0 || !sum.Changed {
		t.Errorf("%d added, %d replaced, %d reused, changed %v, want one added file", sum.Added, sum.Replaced, sum.Reused, sum.Changed)
	}
	if want := ciphertextSize(t, 2<<20); sum.BytesSent != want {
		t.Errorf("sent %d bytes, want the new file's %d", sum.BytesSent, want)
	}
	requireFiles(t, e, vol, expectedFiles(t, dir, ""))
	for _, p := range e.cloud.putLog()[puts:] {
		if p.Path != "extra.bin" {
			t.Errorf("a part of %s was sent", p.Path)
		}
	}
}

// AC 2: the same files in another Volume send no file bytes.
func TestPutReuseAcrossVolumes(t *testing.T) {
	e := newTestEnv(t)
	dir := standardTree(t, e)
	first := e.cloud.newVolume("first", "")
	second := e.cloud.newVolume("second", "")
	want := expectedFiles(t, dir, "")
	e.mustPut(first, dir)
	puts := len(e.cloud.putLog())

	sum := e.mustPut(second, dir)

	requireFiles(t, e, second, want)
	if sum.BytesSent != 0 || sum.Reused != len(want) || sum.Added != len(want) || sum.Replaced != 0 || !sum.Changed {
		t.Errorf("%d bytes, %d reused, %d added, %d replaced, changed %v; want all paths added with reused content", sum.BytesSent, sum.Reused, sum.Added, sum.Replaced, sum.Changed)
	}
	for _, f := range sum.Files {
		if f.Action != ActionAdded {
			t.Errorf("path %s has action %s, want added", f.Path, f.Action)
		}
	}
	if got := len(e.cloud.putLog()); got != puts {
		t.Errorf("%d parts were sent for content the organization already stores", got-puts)
	}
}

func TestPutModeChangeReplacesThePathAndReusesItsContent(t *testing.T) {
	e := newTestEnv(t)
	file := singleFile(t, e, "example.sh", []byte("example"))
	vol := e.cloud.newVolume("model-weights", "")
	before := e.mustPut(vol, file)
	if err := os.Chmod(file, 0o700); err != nil { //nolint:gosec // The fixture needs the executable bit.
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Skip("the test filesystem does not expose the executable bit")
	}

	sum := e.mustPut(vol, file)
	if !sum.Changed || sum.ManifestSHA256 == before.ManifestSHA256 || sum.Added != 0 || sum.Replaced != 1 || sum.Reused != 1 || sum.BytesSent != 0 {
		t.Fatalf("summary = %+v, want one replaced path with reused content and no bytes sent", sum)
	}
	if len(sum.Files) != 1 || sum.Files[0].Action != ActionReplaced {
		t.Fatalf("file results = %+v, want a replaced path", sum.Files)
	}
}

// AC 3: a changed file uploads whole and nothing else does.
func TestPutChangedFile(t *testing.T) {
	e := newTestEnv(t)
	dir := standardTree(t, e)
	vol := e.cloud.newVolume("model-weights", "")
	first := e.mustPut(vol, dir)
	puts, hashed := len(e.cloud.putLog()), e.hashedCount()

	changed := filepath.Join(dir, "model", "weights.bin")
	rewrite(t, changed, patterned(3<<20+500, 9))
	sum := e.mustPut(vol, dir)

	requireFiles(t, e, vol, expectedFiles(t, dir, ""))
	if sum.Replaced != 1 || sum.Added != 0 || sum.Reused != len(first.Files)-1 || !sum.Changed {
		t.Errorf("%d replaced, %d added, %d reused, want one replaced and the rest reused", sum.Replaced, sum.Added, sum.Reused)
	}
	if want := ciphertextSize(t, 3<<20+500); sum.BytesSent != want {
		t.Errorf("sent %d bytes, want the whole changed file, %d", sum.BytesSent, want)
	}
	for _, p := range e.cloud.putLog()[puts:] {
		if p.Path != "model/weights.bin" {
			t.Errorf("a part of %s was sent", p.Path)
		}
	}
	if got := e.hashedCount() - hashed; got != 1 {
		t.Errorf("%d files were hashed again, want only the changed one", got)
	}
	if fr := filesByPath(sum)["model/weights.bin"]; fr.Action != ActionReplaced || fr.BytesSent != sum.BytesSent {
		t.Errorf("the changed file is %+v", fr)
	}
}

// AC 3: a file rewritten at the same size with its modification time put back uploads its new content.
func TestPutReusedFileWithRestoredMtimeUploadsNewContent(t *testing.T) {
	e := newTestEnv(t)
	dir := filepath.Join(e.srcDir, "weights")
	file := filepath.Join(dir, "model.bin")
	writeFile(t, file, patterned(64, 1))
	vol := e.cloud.newVolume("model-weights", "")
	e.mustPut(vol, dir)
	swapKeepingStat(t, file, patterned(64, 2))

	sum := e.mustPut(vol, dir)

	requireFiles(t, e, vol, expectedFiles(t, dir, ""))
	if sum.Replaced != 1 || sum.Reused != 0 {
		t.Errorf("%d replaced, %d reused, want the rewritten file replaced", sum.Replaced, sum.Reused)
	}
}

type statementRefusal struct {
	name         string
	organization string
	refuse       error
	real         bool
}

// AC 4: a refused statement stops the put before it hashes, seals or opens a session.
func TestPutRefusesBadStatementBeforeDeclare(t *testing.T) {
	cases := []statementRefusal{
		{
			name:         "another organization",
			organization: uuid.NewString(),
		},
		{
			name:   "past its not-after",
			refuse: fmt.Errorf("%w: statement is at or past its not-after", keypair.ErrStatement),
		},
		{
			name:   "failing verification",
			refuse: fmt.Errorf("%w: signature does not verify", keypair.ErrStatement),
		},
		{
			name: "the real verifier, which holds no verification keys",
			real: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			dir := standardTree(t, e)
			vol := e.cloud.newVolume("model-weights", "")
			opts := e.options(vol, dir)
			e.keys.refuse = tc.refuse
			if tc.organization != "" {
				opts.Organization = tc.organization
			}
			if tc.real {
				v, err := keypair.NewVerifier(testServerlessURL, e.configDir)
				if err != nil {
					t.Fatal(err)
				}
				opts.Keys = NewKeys(v)
			}

			_, err := Run(t.Context(), opts)

			if !errors.Is(err, keypair.ErrStatement) {
				t.Fatalf("error = %v, want a refused statement", err)
			}
			if n := e.cloud.count("open") + e.cloud.count("declare") + e.cloud.count("abort"); n != 0 {
				t.Errorf("%d upload calls were made after the refusal: %v", n, e.cloud.callLog())
			}
			if e.keys.sealed() != 0 || e.hashedCount() != 0 {
				t.Errorf("%d keys sealed and %d files hashed before the refusal", e.keys.sealed(), e.hashedCount())
			}
			if dirs := e.uploadDirs(); len(dirs) != 0 {
				t.Errorf("resume state written before the refusal: %v", dirs)
			}
		})
	}
}

// recordedOpen keeps the session-open request the engine sent.
type recordedOpen struct {
	API
	open UploadOpen
}

func (r *recordedOpen) OpenUpload(ctx context.Context, vol VolumeRef, open UploadOpen) (Upload, error) {
	r.open = open
	return r.API.OpenUpload(ctx, vol, open)
}

// AC 4: through a real verifier, the engine passes the configured organization to Verify and opens the session with the verified key pair.
func TestPutThroughTheRealVerifier(t *testing.T) {
	t.Run("the configured organization reaches Verify", func(t *testing.T) {
		e := newTestEnv(t)
		dir := standardTree(t, e)
		opts := e.options(e.cloud.newVolume("model-weights", ""), dir)
		opts.Keys = e.realKeys(t)

		if _, err := Run(t.Context(), opts); err != nil {
			t.Fatalf("the real verifier refused the configured organization's statement: %v", err)
		}

		requireFiles(t, e, opts.Volume, expectedFiles(t, dir, ""))
		other := e.options(e.cloud.newVolume("other", ""), dir)
		other.Keys = e.realKeys(t)
		other.Organization = uuid.NewString()
		if _, err := Run(t.Context(), other); !errors.Is(err, keypair.ErrStatement) || !strings.Contains(err.Error(), "organization") {
			t.Errorf("another organization: error = %v, want the statement refused for its organization", err)
		}
	})

	t.Run("the verified key pair reaches OpenUpload", func(t *testing.T) {
		e := newTestEnv(t)
		dir := standardTree(t, e)
		rec := &recordedOpen{API: e.cloud}
		opts := e.options(e.cloud.newVolume("model-weights", ""), dir)
		opts.API = rec
		opts.Keys = e.realKeys(t)

		_, err := Run(t.Context(), opts)

		if want := uuid.UUID(e.vec.keyPair).String(); rec.open.KeyPairID != want {
			t.Fatalf("the session opened with key pair %q, want the verified statement's %q", rec.open.KeyPairID, want)
		}
		if err != nil {
			t.Fatal(err)
		}
	})
}

// AC 4: put without an organization refuses before any work.
func TestPutRequiresOrganization(t *testing.T) {
	t.Setenv("RUNWARE_DASHBOARD_URL", "")
	for _, org := range []string{"", "  ", "not-a-uuid"} {
		t.Run("organization "+org, func(t *testing.T) {
			e := newTestEnv(t)
			vol := e.cloud.newVolume("model-weights", "")
			opts := e.options(vol, filepath.Join(e.srcDir, "does-not-exist"))
			opts.Organization = org

			_, err := Run(t.Context(), opts)

			if err == nil {
				t.Fatal("put ran without an organization")
			}
			if strings.TrimSpace(org) == "" && !errors.Is(err, ErrNoOrganization) {
				t.Errorf("error = %v, want ErrNoOrganization", err)
			}
			for _, want := range []string{config.OrganizationFromEnvVar, "organization", config.DefaultDashboardURL} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
			if calls := e.cloud.callLog(); len(calls) != 0 {
				t.Errorf("the API was called: %v", calls)
			}
		})
	}
}

// AC 5: the text summary.
func TestPutSummaryText(t *testing.T) {
	e := newTestEnv(t)
	dir := standardTree(t, e)
	vol := e.cloud.newVolume("model-weights", "")

	first := e.mustPut(vol, dir)
	var text bytes.Buffer
	if err := first.WriteText(&text); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Uploaded to Volume model-weights (production)",
		"Latest files digest: " + first.ManifestSHA256,
		"Files: 4 added, 0 replaced, 0 reused",
		"Bytes sent: " + strconv.FormatInt(first.BytesSent, 10),
	} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("summary lacks %q:\n%s", want, text.String())
		}
	}

	rewrite(t, filepath.Join(dir, "config.json"), patterned(20, 5))
	replaced := e.mustPut(vol, dir)
	text.Reset()
	if err := replaced.WriteText(&text); err != nil {
		t.Fatal(err)
	}
	if want := "Files: 0 added, 1 replaced, 3 reused"; !strings.Contains(text.String(), want) {
		t.Errorf("summary lacks %q:\n%s", want, text.String())
	}

	repeat := e.mustPut(vol, dir)
	text.Reset()
	if err := repeat.WriteText(&text); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"No change to Volume model-weights", "Latest files digest: " + repeat.ManifestSHA256, "Bytes sent: 0 (0 B)"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("no-op summary lacks %q:\n%s", want, text.String())
		}
	}
}

// requireSummaryKeys pins the literal JSON keys of the summary, which other tools read.
func requireSummaryKeys(t *testing.T, raw []byte) {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	wantSummary := []string{"added", "bytesSent", "changed", "environment", "files", "manifestSha256", "replaced", "reused", "volume"}
	if got := slices.Sorted(maps.Keys(obj)); !slices.Equal(got, wantSummary) {
		t.Errorf("summary keys = %v, want %v", got, wantSummary)
	}
	files, _ := obj["files"].([]any)
	if len(files) == 0 {
		t.Fatal("the JSON summary lists no files")
	}
	file, _ := files[0].(map[string]any)
	wantFile := []string{"action", "bytesSent", "path", "sha256", "size"}
	if got := slices.Sorted(maps.Keys(file)); !slices.Equal(got, wantFile) {
		t.Errorf("file keys = %v, want %v", got, wantFile)
	}
}

// AC 5: the JSON summary matches the Volume's latest files.
func TestPutSummaryJSON(t *testing.T) {
	e := newTestEnv(t)
	dir := standardTree(t, e)
	vol := e.cloud.newVolume("model-weights", "")
	sum := e.mustPut(vol, dir)

	var buf bytes.Buffer
	if err := output.PrintTo(&buf, output.FormatJSON, sum); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	var got Summary
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&got); err != nil {
		t.Fatalf("the JSON summary does not decode into the summary: %v", err)
	}
	requireSummaryKeys(t, raw)
	latest, digest := e.cloud.latest(vol)
	if got.ManifestSHA256 != digest || got.Volume != "model-weights" || got.Environment != "production" || !got.Changed {
		t.Errorf("summary header = %+v, want the Volume's digest %q", got, digest)
	}
	if len(got.Files) != len(latest) {
		t.Fatalf("%d files in the summary, %d in the Volume", len(got.Files), len(latest))
	}
	var sent int64
	for i, f := range got.Files {
		l := latest[i]
		if f.Path != l.Path || f.Size != l.Size || f.SHA256 != l.SHA256 || f.Action != ActionAdded {
			t.Errorf("summary file %+v does not match the Volume's %+v", f, l)
		}
		if f.BytesSent != ciphertextSize(t, l.Size) {
			t.Errorf("%s: %d bytes sent, want %d", f.Path, f.BytesSent, ciphertextSize(t, l.Size))
		}
		sent += f.BytesSent
	}
	if got.BytesSent != sent || got.Added+got.Replaced+got.Reused != len(latest) {
		t.Errorf("totals: %d bytes sent of %d, %d+%d+%d files of %d", got.BytesSent, sent, got.Added, got.Replaced, got.Reused, len(latest))
	}
}

// AC 6: the help states the additive rules and promises no deletion.
func TestPutHelpText(t *testing.T) {
	help := strings.Join(strings.Fields(HelpShort+" "+HelpLong+" "+HelpExample), " ")
	for _, want := range []string{
		"Uploads are additive",
		"files you do not name stay as they are",
		"Workers that start after an upload get the new files",
		"Rolling an app back restores its image and settings, not earlier Volume files",
		"create a new Volume",
	} {
		if !strings.Contains(help, want) {
			t.Errorf("help lacks %q", want)
		}
	}
	for _, banned := range []string{"delet", "remov", "prune", "sync"} {
		if strings.Contains(strings.ToLower(help), banned) {
			t.Errorf("help mentions %q, which reads as a promise to take files out of a Volume", banned)
		}
	}
}

type changeCase struct {
	name      string
	hook      func(t *testing.T, e *testEnv, file string)
	noDeclare bool
}

// swapKeepingStat changes a file's content, not its size or modification time, until a coarse file clock moves its change time.
func swapKeepingStat(t *testing.T, p string, content []byte) {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	before := changeTime(p, info)
	for {
		if err := os.WriteFile(p, content, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, info.ModTime(), info.ModTime()); err != nil {
			t.Fatal(err)
		}
		now, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if before == 0 || changeTime(p, now) != before {
			return
		}
		time.Sleep(time.Millisecond)
	}
}

// appendTo adds bytes to a file, moving its size and modification time.
func appendTo(t *testing.T, p string, extra []byte) {
	t.Helper()
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(extra); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	later := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
}

// tamperedAnswers edits declaration answers, as a server that broke the contract would.
type tamperedAnswers struct {
	API
	edit func(*DeclarationResult)
}

func (a tamperedAnswers) DeclareFiles(ctx context.Context, up UploadRef, files FileDeclarations) (DeclarationResult, error) {
	res, err := a.API.DeclareFiles(ctx, up, files)
	if err == nil {
		a.edit(&res)
	}
	return res, err
}

type answerCase struct {
	name string
	edit func(*DeclarationResult)
}

func TestPutAbortsOnBrokenAnswers(t *testing.T) {
	cases := []answerCase{
		{
			name: "fewer answers than files",
			edit: func(r *DeclarationResult) { r.Files = r.Files[:0] },
		},
		{
			name: "an answer for another file",
			edit: func(r *DeclarationResult) { r.Files[0].File.Path = "other/path.bin" },
		},
		{
			name: "an answer without a file id",
			edit: func(r *DeclarationResult) { r.Files[0].File.ID = "" },
		},
		{
			name: "a file declared as failed",
			edit: func(r *DeclarationResult) { r.Files[0].File.State = "failed" },
		},
		{
			name: "an uploading file without targets",
			edit: func(r *DeclarationResult) { r.Files[0].UploadTargets = nil },
		},
		{
			name: "a target for a part beyond the layout",
			edit: func(r *DeclarationResult) { r.Files[0].UploadTargets[0].PartNumber = 99 },
		},
		{
			name: "a target for part zero",
			edit: func(r *DeclarationResult) { r.Files[0].UploadTargets[0].PartNumber = 0 },
		},
		{
			name: "a layout that is not the format's",
			edit: func(r *DeclarationResult) { r.Files[0].File.Layout.PartCount++ },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			file := singleFile(t, e, "weights.bin", patterned(5000, 1))
			vol := e.cloud.newVolume("model-weights", "")
			opts := e.options(vol, file)
			opts.API = tamperedAnswers{
				API:  e.cloud,
				edit: tc.edit,
			}

			_, err := Run(t.Context(), opts)

			if !errors.Is(err, ErrProtocol) {
				t.Fatalf("error = %v, want a contract violation", err)
			}
			if e.cloud.count("abort") != 1 || e.cloud.count("complete") != 0 || len(e.cloud.putLog()) != 0 {
				t.Errorf("calls = %v, parts = %d, want an abort and nothing sent", e.cloud.callLog(), len(e.cloud.putLog()))
			}
			if !e.keys.allCleared() || len(e.uploadDirs()) != 0 {
				t.Errorf("keys cleared = %v, resume state = %v", e.keys.allCleared(), e.uploadDirs())
			}
		})
	}
}

// AC 7: a file changed after hashing aborts the session and the error names it.
func TestPutSourceChangedAborts(t *testing.T) {
	cases := []changeCase{
		{
			name: "after hashing, before it is declared",
			hook: func(t *testing.T, e *testEnv, file string) {
				e.cloud.onOpen = func() { rewrite(t, file, patterned(100, 8)) }
			},
			noDeclare: true,
		},
		{
			name: "after it is declared, with the same size and time",
			hook: func(t *testing.T, e *testEnv, file string) {
				e.cloud.onDeclare = func(FileDeclarations) { swapKeepingStat(t, file, patterned(64, 8)) }
			},
		},
		{
			name: "after it is declared, by appending to it",
			hook: func(t *testing.T, e *testEnv, file string) {
				e.cloud.onDeclare = func(FileDeclarations) { appendTo(t, file, patterned(10, 8)) }
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			file := filepath.Join(e.srcDir, "model", "weights.bin")
			other := filepath.Join(e.srcDir, "model", "other.bin")
			writeFile(t, file, patterned(64, 1))
			writeFile(t, other, patterned(64, 2))
			vol := e.cloud.newVolume("model-weights", "")
			tc.hook(t, e, file)

			_, err := e.put(vol, filepath.Dir(file))

			assertAborted(t, e, err, file, vol, 0)
			if tc.noDeclare && e.cloud.count("declare") != 0 {
				t.Error("a file that changed after it was hashed was still declared")
			}
			for _, p := range e.cloud.putLog() {
				if p.Path == "model/weights.bin" {
					t.Errorf("part %d of the changed file was sent", p.Part)
				}
			}
			fe, ok := errors.AsType[*FileError](err)
			if !ok {
				t.Fatalf("error %v does not name a file", err)
			}
			if _, cached := loadHashCache(e.stateDir()).entries[fe.Path]; cached {
				t.Error("the changed file's hash is still cached, so a rerun would not start over")
			}
		})
	}
}

// AC 7: a reused file is checked by size and time too.
func TestPutReusedFileChangedAborts(t *testing.T) {
	e := newTestEnv(t)
	file := filepath.Join(e.srcDir, "model", "weights.bin")
	writeFile(t, file, patterned(64, 1))
	e.mustPut(e.cloud.newVolume("first", ""), file)
	second := e.cloud.newVolume("second", "")
	e.cloud.onDeclare = func(FileDeclarations) { rewrite(t, file, patterned(65, 2)) }
	since := len(e.cloud.callLog())

	_, err := e.put(second, file)

	assertAborted(t, e, err, file, second, since)
}

// AC 7: a file truncated during the upload is a changed file.
func TestPutTruncatedDuringUploadAborts(t *testing.T) {
	e := newTestEnv(t)
	file := filepath.Join(e.srcDir, "big.bin")
	writeTwoPartFile(t, file)
	vol := e.cloud.newVolume("model-weights", "")
	e.cloud.onDeclare = func(FileDeclarations) {
		if err := os.Truncate(file, 1000); err != nil {
			t.Error(err)
		}
	}

	_, err := e.put(vol, file)

	assertAborted(t, e, err, file, vol, 0)
}

// assertAborted checks the file is named, the session aborted and nothing published or left behind.
func assertAborted(t *testing.T, e *testEnv, err error, file string, vol VolumeRef, since int) {
	t.Helper()
	if !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("error = %v, want a changed source", err)
	}
	resolved, evalErr := filepath.EvalSymlinks(file)
	if evalErr != nil {
		t.Fatal(evalErr)
	}
	if !strings.Contains(err.Error(), resolved) {
		t.Errorf("error %q does not name %s", err, resolved)
	}
	calls := e.cloud.callLog()[since:]
	if !slices.Contains(calls, "abort") || slices.Contains(calls, "complete") || e.cloud.uploadState() != fakeAborted {
		t.Errorf("calls = %v, state = %q, want the session aborted and never completed", calls, e.cloud.uploadState())
	}
	if dirs := e.uploadDirs(); len(dirs) != 0 {
		t.Errorf("resume state left after the abort: %v", dirs)
	}
	if files, _ := e.cloud.latest(vol); len(files) != 0 {
		t.Errorf("an aborted session published %d files", len(files))
	}
	if !e.keys.allCleared() {
		t.Error("a session or file key was not cleared after the abort")
	}
}
