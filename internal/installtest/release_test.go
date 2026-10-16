package installtest_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"maps"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/runware/runware-cli/internal/installtest"
)

// releaseStub is the binary content the tests put in a fake release.
const releaseStub = "#!/bin/sh\necho stub\n"

// TestNewRelease_ServesEveryArchiveWithItsChecksum downloads each archive the
// fake release lists, as an install script would, and checks that the
// archive holds the stub as its binary at the root, and that checksums.txt
// has the archive's digest, or a wrong one when the release is tampered.
func TestNewRelease_ServesEveryArchiveWithItsChecksum(t *testing.T) {
	t.Parallel()

	tests := []releaseCase{
		{
			name: "correct checksums",
		},
		{
			name:        "bad checksums",
			badChecksum: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			release := installtest.NewRelease(t, installtest.ReleaseConfig{
				Tag:         "v1.2.3",
				Stub:        releaseStub,
				BadChecksum: tt.badChecksum,
			})
			download := release.URL + "/download/v1.2.3/"

			digests := map[string]string{}
			for _, archive := range installtest.ReleaseArchives() {
				body := get(t, download+archive.Name)
				files := unpack(t, archive.Name, body)
				want := map[string]string{
					archive.Binary: releaseStub,
				}
				if !maps.Equal(files, want) {
					t.Errorf("%s holds %q, want %q", archive.Name, files, want)
				}
				sum := sha256.Sum256(body)
				digests[archive.Name] = hex.EncodeToString(sum[:])
			}

			listed := parseChecksums(t, get(t, download+installtest.ChecksumsName))
			if !sameFileNames(listed, digests) {
				t.Fatalf("%s lists %q, want one entry for each of %q", installtest.ChecksumsName, sortedFileNames(listed), sortedFileNames(digests))
			}
			wantMatch := !tt.badChecksum
			for name, digest := range digests {
				if match := listed[name] == digest; match != wantMatch {
					t.Errorf("%s lists %s for %s, the archive's digest is %s, want match = %t", installtest.ChecksumsName, listed[name], name, digest, wantMatch)
				}
			}
		})
	}
}

// releaseCase is one shape of fake release.
type releaseCase struct {
	name        string
	badChecksum bool
}

// get returns the body of a GET of url and fails the test unless the status
// is 200.
func get(t *testing.T, url string) []byte {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request for %s: %v", url, err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get %s: status %d", url, resp.StatusCode)
	}
	return body
}

// unpack returns the content of each file in the archive, by path. It reads
// a zip or a tarball by the archive's name.
func unpack(t *testing.T, name string, body []byte) map[string]string {
	t.Helper()

	files := map[string]string{}
	switch {
	case strings.HasSuffix(name, ".zip"):
		zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		for _, f := range zr.File {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open %s in %s: %v", f.Name, name, err)
			}
			data, err := io.ReadAll(rc)
			_ = rc.Close()
			if err != nil {
				t.Fatalf("read %s in %s: %v", f.Name, name, err)
			}
			files[f.Name] = string(data)
		}
	case strings.HasSuffix(name, ".tar.gz"):
		gz, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			t.Fatalf("open %s: %v", name, err)
		}
		tr := tar.NewReader(gz)
		for {
			hdr, err := tr.Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			data, err := io.ReadAll(tr)
			if err != nil {
				t.Fatalf("read %s in %s: %v", hdr.Name, name, err)
			}
			files[hdr.Name] = string(data)
		}
	default:
		t.Fatalf("%s is not a zip or a tarball", name)
	}
	return files
}

// parseChecksums returns the digest of each file in a checksums.txt, by file
// name.
func parseChecksums(t *testing.T, body []byte) map[string]string {
	t.Helper()

	digests := map[string]string{}
	for line := range strings.Lines(string(body)) {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("checksums line %q does not have 2 fields", line)
		}
		digests[fields[1]] = fields[0]
	}
	return digests
}

// sameFileNames reports whether two maps keyed by file name have the same
// file names.
func sameFileNames(a, b map[string]string) bool {
	return maps.EqualFunc(a, b, func(string, string) bool { return true })
}

// sortedFileNames returns the file names that key m, sorted.
func sortedFileNames(m map[string]string) []string {
	return slices.Sorted(maps.Keys(m))
}
