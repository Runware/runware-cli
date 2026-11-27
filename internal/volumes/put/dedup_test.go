package put

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestPutDeduplicatesAcrossDeclarationBatches(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	vol := e.cloud.newVolume("weights", "")
	content := []byte("common")
	first := singleFile(t, e, "aaa.bin", content)
	_ = singleFile(t, e, "aab.bin", content)
	last := singleFile(t, e, "zzz.bin", content)
	if runtime.GOOS != goosWindows {
		if err := os.Chmod(last, 0o700); err != nil { //nolint:gosec // test the executable file mode
			t.Fatal(err)
		}
	}
	for i := range maxDeclarationBatch + 1 {
		_ = singleFile(t, e, fmt.Sprintf("file-%03d.bin", i), fmt.Appendf(nil, "%06d", i))
	}
	var batches []int
	e.cloud.onDeclare = func(batch FileDeclarations) { batches = append(batches, len(batch.Files)) }

	dir := filepath.Dir(first)
	sum := e.mustPut(vol, dir)
	want := expectedFiles(t, dir, "")
	requireFiles(t, e, vol, want)
	if !slices.Equal(batches, []int{maxDeclarationBatch, 2, 2}) {
		t.Fatalf("declaration sizes = %v, want representatives first in bounded batches", batches)
	}
	byPath := filesByPath(sum)
	bytesPerBlob := ciphertextSize(t, int64(len(content)))
	if sum.Reused != 2 || sum.Added != len(want) || sum.BytesSent != int64(len(want)-2)*bytesPerBlob ||
		byPath["aaa.bin"].BytesSent != bytesPerBlob || byPath["aab.bin"].BytesSent != 0 || byPath["zzz.bin"].BytesSent != 0 {
		t.Fatalf("summary totals = %d bytes, %d added, %d reused, want two reused paths with zero transfer",
			sum.BytesSent, sum.Added, sum.Reused)
	}
	if okPuts(e, "aaa.bin", 1) != 1 || okPuts(e, "aab.bin", 1) != 0 || okPuts(e, "zzz.bin", 1) != 0 {
		t.Fatal("the fake Worker must receive only the representative of the shared content")
	}
	for i, file := range want {
		if sum.Files[i].Path != file.Path {
			t.Fatalf("summary order at %d = %q, want %q", i, sum.Files[i].Path, file.Path)
		}
	}
	var out bytes.Buffer
	if err := sum.WriteText(&out); err != nil || !strings.Contains(out.String(), fmt.Sprintf("Bytes sent: %d (", sum.BytesSent)) {
		t.Fatalf("text summary = %q, err = %v", out.String(), err)
	}
	raw, err := json.Marshal(sum)
	if err != nil || !bytes.Contains(raw, fmt.Appendf(nil, `"bytesSent":%d`, sum.BytesSent)) || !bytes.Contains(raw, []byte(`"reused":2`)) {
		t.Fatalf("JSON summary did not count transfer and reuse: %v", err)
	}
	if again := e.mustPut(vol, dir); again.BytesSent != 0 || again.Reused != len(want) || again.Changed || again.ManifestSHA256 != sum.ManifestSHA256 {
		t.Fatalf("repeat sent %d bytes, reused %d files, changed %v, digest %q",
			again.BytesSent, again.Reused, again.Changed, again.ManifestSHA256)
	}
}

func TestDeduplicatedPutRefusesChangedDuplicate(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	vol := e.cloud.newVolume("weights", "")
	first := singleFile(t, e, "first.txt", []byte("common"))
	second := singleFile(t, e, "second.txt", []byte("common"))
	e.cloud.onCompleteFile = func(path string) {
		if path == "first.txt" {
			rewrite(t, second, []byte("edited"))
		}
	}
	_, err := e.put(vol, first, second)
	if !errors.Is(err, ErrSourceChanged) || !strings.Contains(err.Error(), second) {
		t.Fatalf("put = %v, want a source change naming %q", err, second)
	}
	if files, _ := e.cloud.latest(vol); len(files) != 0 || e.cloud.count("abort") != 1 || e.cloud.count("declare") != 1 {
		t.Fatalf("files = %v, calls = %v, want an abort before declaring the changed duplicate", files, e.cloud.callLog())
	}
}

func TestDeduplicatedPutDoesNotDeclareDuplicateIfRepresentativeFails(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	vol := e.cloud.newVolume("weights", "")
	first := singleFile(t, e, "first.txt", []byte("common"))
	second := singleFile(t, e, "second.txt", []byte("common"))
	e.cloud.fault = func(partRequest) *fakeFault {
		return &fakeFault{
			Status: http.StatusBadRequest,
			Slug:   slugPartLengthMismatch,
		}
	}
	if _, err := e.put(vol, first, second); !errors.Is(err, ErrPartLengthMismatch) {
		t.Fatalf("put = %v, want permanent part refusal", err)
	}
	if files, _ := e.cloud.latest(vol); len(files) != 0 || e.cloud.count("abort") != 1 || e.cloud.count("declare") != 1 {
		t.Fatalf("files = %v, calls = %v, want abort before declaring the duplicate", files, e.cloud.callLog())
	}
}

type interruptFileCompletion struct {
	API
	cancel       context.CancelFunc
	dropResponse bool
}

func (a interruptFileCompletion) CompleteFile(ctx context.Context, ref UploadRef, id string, completion FileCompletion) (UploadFile, error) {
	file, err := a.API.CompleteFile(ctx, ref, id, completion)
	if err != nil {
		return UploadFile{}, err
	}
	a.cancel()
	if a.dropResponse {
		return UploadFile{}, context.Cause(ctx)
	}
	return file, nil
}

func TestDeduplicatedPutResumesBeforeDeclaringDuplicate(t *testing.T) {
	t.Parallel()
	for _, drop := range []bool{false, true} {
		t.Run(fmt.Sprintf("drop response %v", drop), func(t *testing.T) {
			e := newTestEnv(t)
			vol := e.cloud.newVolume("weights", "")
			first := singleFile(t, e, "first.txt", []byte("common"))
			second := singleFile(t, e, "second.txt", []byte("common"))
			opts := e.options(vol, first, second)
			var seals sealRecorder
			opts.newSealer = seals.factory
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			opts.API = interruptFileCompletion{
				API:          e.cloud,
				cancel:       cancel,
				dropResponse: drop,
			}
			if _, err := Run(ctx, opts); !errors.Is(err, context.Canceled) {
				t.Fatalf("put = %v, want an interruption", err)
			}
			if files, _ := e.cloud.latest(vol); len(files) != 0 || e.cloud.count("declare") != 1 {
				t.Fatalf("files = %v, calls = %v, want one verified file and no publication", files, e.cloud.callLog())
			}
			opts.API = e.cloud
			sum, err := Run(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			if sum.BytesSent != 0 || sum.Added != 2 || sum.Reused != 1 || len(sum.Files) != 2 || sum.Files[0].Reused || !sum.Files[1].Reused ||
				e.cloud.count("open") != 1 || e.cloud.count("complete") != 1 || len(e.cloud.putLog()) != 1 {
				t.Fatalf("summary = %+v, calls = %v, want reuse without resending the verified content", sum, e.cloud.callLog())
			}
			var out bytes.Buffer
			if err := sum.WriteText(&out); err != nil || !strings.Contains(out.String(),
				fmt.Sprintf("second.txt  6 bytes  0 sent  sha256 %s  reused", sum.Files[1].SHA256)) {
				t.Fatalf("text summary = %q, err = %v", out.String(), err)
			}
			requireFiles(t, e, vol, expectedFiles(t, filepath.Dir(first), ""))
			seals.requireSafe(t)
		})
	}
}

func TestDeduplicatedPutFallsBackWhenBlobBecomesIneligible(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	vol := e.cloud.newVolume("weights", "")
	first := singleFile(t, e, "first.txt", []byte("common"))
	second := singleFile(t, e, "second.txt", []byte("common"))
	e.cloud.onDeclare = func(batch FileDeclarations) {
		if d := batch.Files[0]; d.Path == "second.txt" {
			e.cloud.mu.Lock()
			delete(e.cloud.blobs, blobKey{
				sha:  d.SHA256,
				size: d.Size,
			})
			e.cloud.mu.Unlock()
		}
	}
	sum := e.mustPut(vol, first, second)
	if sum.Reused != 0 || len(e.cloud.putLog()) != 2 || e.cloud.count("complete") != 1 {
		t.Fatalf("summary = %+v, calls = %v, want both files uploaded and published", sum, e.cloud.callLog())
	}
}

func TestDeduplicatedResumeRefusesChangedUndeclaredSource(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	vol := e.cloud.newVolume("weights", "")
	first := singleFile(t, e, "first.txt", []byte("common"))
	second := singleFile(t, e, "second.txt", []byte("common"))
	opts := e.options(vol, first, second)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	opts.API = interruptFileCompletion{
		API:    e.cloud,
		cancel: cancel,
	}
	if _, err := Run(ctx, opts); !errors.Is(err, context.Canceled) {
		t.Fatalf("first put = %v, want an interruption", err)
	}
	rewrite(t, second, []byte("edited"))

	opts.API = e.cloud
	if _, err := Run(t.Context(), opts); !errors.Is(err, ErrSourceChanged) || !strings.Contains(err.Error(), second) {
		t.Fatalf("resume = %v, want a source-change error naming %q", err, second)
	}
	if files, _ := e.cloud.latest(vol); len(files) != 0 || e.cloud.count("abort") != 1 || e.cloud.count("open") != 1 {
		t.Fatalf("files = %v, calls = %v, want the saved session aborted without publication", files, e.cloud.callLog())
	}
}

type lostDeclarations struct {
	API
	lost map[string]struct{}
}

func (a lostDeclarations) DeclareFiles(ctx context.Context, ref UploadRef, batch FileDeclarations) (DeclarationResult, error) {
	result, err := a.API.DeclareFiles(ctx, ref, batch)
	if err != nil {
		return DeclarationResult{}, err
	}
	path := batch.Files[0].Path
	if _, ok := a.lost[path]; !ok {
		a.lost[path] = struct{}{}
		return DeclarationResult{}, io.ErrUnexpectedEOF
	}
	return result, nil
}

func TestDeduplicatedPutReplaysBothDeclarations(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	vol := e.cloud.newVolume("weights", "")
	first := singleFile(t, e, "first.txt", []byte("common"))
	second := singleFile(t, e, "second.txt", []byte("common"))
	opts := e.options(vol, first, second)
	opts.API = lostDeclarations{
		API:  e.cloud,
		lost: map[string]struct{}{},
	}
	sum, err := Run(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Reused != 1 || e.cloud.count("declare") != 4 || len(e.cloud.putLog()) != 1 || e.cloud.count("complete") != 1 {
		t.Fatalf("summary = %+v, calls = %v, want each declaration safely replayed", sum, e.cloud.callLog())
	}
}

func TestDeduplicatedPutPreservesLegacyDeclaredAttempts(t *testing.T) {
	t.Parallel()
	e := newTestEnv(t)
	vol := e.cloud.newVolume("weights", "")
	first := singleFile(t, e, "first.txt", []byte("common"))
	second := singleFile(t, e, "second.txt", []byte("common"))
	opts := e.options(vol, first, second)
	saveLegacyDeclarations(t, opts)

	sum, err := Run(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Reused != 0 || len(e.cloud.putLog()) != 2 || e.cloud.count("open") != 1 {
		t.Fatalf("summary = %+v, calls = %v, want both original attempts completed", sum, e.cloud.callLog())
	}
}

func saveLegacyDeclarations(t *testing.T, opts Options) {
	t.Helper()
	ctx := t.Context()
	r, err := newRunner(opts)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := Walk(opts.Paths, opts.To)
	if err != nil {
		t.Fatal(err)
	}
	r.o.onHash = nil
	if err := r.hashSources(ctx, sources, loadHashCache(r.stateDir)); err != nil {
		t.Fatal(err)
	}
	sess, err := r.openKeys(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Clear()
	up, err := r.api.OpenUpload(ctx, opts.Volume, UploadOpen{
		KeyPairID:     uuid.UUID(sess.KeyPairID()).String(),
		Encapsulation: base64.RawURLEncoding.EncodeToString(sess.Encapsulation()),
	})
	if err != nil {
		t.Fatal(err)
	}
	r.ref = UploadRef{
		Volume: opts.Volume,
		ID:     up.ID,
	}
	r.invocation = ""
	r.endpoint = ""
	if err := r.begin(sess, uuid.MustParse(up.ID), up); err != nil {
		t.Fatal(err)
	}
	defer r.journal.release()
	var files []*fileState
	for i := range sources {
		files = append(files, &fileState{
			index: i,
			src:   &sources[i],
		})
	}
	defer clearKeys(files)
	if err := r.declare(ctx, sess.SealFile, files); err != nil {
		t.Fatal(err)
	}
	if err := r.journal.close(); err != nil {
		t.Fatal(err)
	}
}
