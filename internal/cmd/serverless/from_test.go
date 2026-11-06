package serverless

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// archiveEntry is one file to put in a test archive. mode is optional, and a
// zero mode packs an ordinary file.
type archiveEntry struct {
	name string
	body string
	mode os.FileMode
}

// buildTestArchive zips the entries exactly as named, including names a packer
// would never produce, so the unpacker can be tested against them.
func buildTestArchive(t *testing.T, entries []archiveEntry) []byte {
	t.Helper()

	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for _, entry := range entries {
		header := &zip.FileHeader{
			Name:   entry.name,
			Method: zip.Deflate,
		}
		if entry.mode != 0 {
			header.SetMode(entry.mode)
		}
		w, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(entry.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// serveArchive hands the bytes back over HTTP and returns the URL.
func serveArchive(t *testing.T, archive []byte) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(archive)
	}))
	t.Cleanup(server.Close)
	return server.URL + "/echo.zip"
}

func TestFetchSourceArchive(t *testing.T) {
	archive := buildTestArchive(t, []archiveEntry{
		{name: manifestFile, body: codeManifest},
		{name: testEntryFile, body: testPySource},
		{name: testNestedFile, body: "y = 2\n"},
	})

	dir, cleanup, err := fetchSourceArchive(context.Background(), serveArchive(t, archive))
	if err != nil {
		t.Fatalf("fetchSourceArchive: %v", err)
	}
	defer cleanup()

	manifest, err := readDeployManifest(dir)
	if err != nil {
		t.Fatalf("readDeployManifest: %v", err)
	}
	if manifest.ID != "resumable-job" {
		t.Errorf("id = %q", manifest.ID)
	}

	// A nested file has to survive, because an entry file is allowed to import
	// its own modules.
	nested, err := os.ReadFile(filepath.Join(dir, "lib", "helpers.py"))
	if err != nil {
		t.Fatalf("read the nested file: %v", err)
	}
	if string(nested) != "y = 2\n" {
		t.Errorf("nested file = %q", nested)
	}

	cleanup()
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("cleanup left %s behind", dir)
	}
}

// The archive comes off the network, so an entry that writes outside the
// directory we unpack into has to be refused rather than followed.
func TestFetchSourceArchiveRefusesEscapingEntries(t *testing.T) {
	for _, tc := range []struct {
		name  string
		entry string
		want  string
	}{
		{
			name:  "parent traversal",
			entry: "../escaped.py",
			want:  "points outside it",
		},
		{
			name:  "deep traversal",
			entry: "nested/../../escaped.py",
			want:  "points outside it",
		},
		{
			name:  "absolute path",
			entry: "/etc/passwd",
			want:  "absolute path",
		},
		{
			// A backslash is an ordinary filename character on POSIX, so a
			// traversal written this way would pass a slash-only check.
			name:  "backslash traversal",
			entry: `..\..\escaped.py`,
			want:  "not a valid path",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			archive := buildTestArchive(t, []archiveEntry{
				{name: manifestFile, body: codeManifest},
				{name: tc.entry, body: "import os\n"},
			})

			_, cleanup, err := fetchSourceArchive(context.Background(), serveArchive(t, archive))
			if cleanup != nil {
				cleanup()
			}
			if err == nil {
				t.Fatalf("%q was accepted", tc.entry)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// A symlink would be read by the packer that walks this directory back, and
// could name any file on the machine running the deploy.
func TestFetchSourceArchiveRefusesSymlink(t *testing.T) {
	archive := buildTestArchive(t, []archiveEntry{
		{name: manifestFile, body: codeManifest},
		{name: testEntryFile, body: "/etc/passwd", mode: os.ModeSymlink | 0o777},
	})

	_, cleanup, err := fetchSourceArchive(context.Background(), serveArchive(t, archive))
	if cleanup != nil {
		cleanup()
	}
	if err == nil {
		t.Fatal("a symlink was accepted")
	}
	if !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("error = %v", err)
	}
}

// The byte caps do not bound the file count, and a few hundred thousand empty
// entries cost almost nothing to send.
func TestFetchSourceArchiveRefusesTooManyEntries(t *testing.T) {
	entries := make([]archiveEntry, 0, maxArchiveEntries+2)
	entries = append(entries, archiveEntry{
		name: manifestFile,
		body: codeManifest,
	})
	for i := range maxArchiveEntries + 1 {
		entries = append(entries, archiveEntry{
			name: fmt.Sprintf("pad/%d.py", i),
			body: "",
		})
	}

	_, cleanup, err := fetchSourceArchive(context.Background(), serveArchive(t, buildTestArchive(t, entries)))
	if cleanup != nil {
		cleanup()
	}
	if err == nil {
		t.Fatal("an archive over the entry cap was accepted")
	}
	if !strings.Contains(err.Error(), "more than the") {
		t.Errorf("error = %v", err)
	}
}

func TestFetchSourceArchiveRejectsNonHTTP(t *testing.T) {
	for _, url := range []string{
		"file:///etc/passwd",
		"ftp://example.com/echo.zip",
		"/local/echo.zip",
	} {
		_, _, err := fetchSourceArchive(context.Background(), url)
		if err == nil {
			t.Fatalf("%q was accepted", url)
		}
		if !strings.Contains(err.Error(), "http or https") {
			t.Errorf("%q: error = %v", url, err)
		}
	}
}

func TestFetchSourceArchiveReportsStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	_, _, err := fetchSourceArchive(context.Background(), server.URL+"/missing.zip")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "answered 404") {
		t.Errorf("error = %v", err)
	}
}

func TestFetchSourceArchiveRejectsNonZip(t *testing.T) {
	_, _, err := fetchSourceArchive(context.Background(), serveArchive(t, []byte("<!doctype html>\n")))
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "read the fetched archive") {
		t.Errorf("error = %v", err)
	}
}

// An archive past the source-upload cap cannot be deployed, so it is refused
// before it is held in memory rather than after the platform rejects it.
func TestFetchSourceArchiveRejectsOversizedDownload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		chunk := make([]byte, 1<<20)
		for range (maxArchiveBytes >> 20) + 1 {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	_, _, err := fetchSourceArchive(context.Background(), server.URL+"/big.zip")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "larger than") {
		t.Errorf("error = %v", err)
	}
}

func TestSafeArchivePathKeepsOrdinaryNames(t *testing.T) {
	for _, name := range []string{testEntryFile, testNestedFile, "./model.py", "a/b/c/d.txt"} {
		got, err := safeArchivePath(name)
		if err != nil {
			t.Errorf("%q: %v", name, err)
			continue
		}
		if filepath.IsAbs(got) || strings.HasPrefix(got, "..") {
			t.Errorf("%q cleaned to %q", name, got)
		}
	}
	if _, err := safeArchivePath(""); err == nil {
		t.Error("an empty name was accepted")
	}
}

// The packer archives an executable entry as 0755, so an entrypoint script that
// arrives executable has to still be executable when the packer walks it back.
func TestFetchSourceArchiveKeepsTheExecutableBit(t *testing.T) {
	if runtime.GOOS == goosWindows {
		t.Skip("no executable bit to preserve on Windows")
	}
	archive := buildTestArchive(t, []archiveEntry{
		{name: manifestFile, body: codeManifest},
		{name: testEntryFile, body: testPySource},
		{name: "entrypoint.sh", body: "#!/bin/sh\nexec python model.py\n", mode: 0o755},
		{name: "notes.txt", body: "plain\n", mode: 0o644},
	})

	dir, cleanup, err := fetchSourceArchive(context.Background(), serveArchive(t, archive))
	if err != nil {
		t.Fatalf("fetchSourceArchive: %v", err)
	}
	defer cleanup()

	executable, err := os.Stat(filepath.Join(dir, "entrypoint.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if executable.Mode().Perm()&0o100 == 0 {
		t.Errorf("entrypoint.sh came out %v, with no owner execute bit", executable.Mode().Perm())
	}

	plain, err := os.Stat(filepath.Join(dir, "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if plain.Mode().Perm()&0o111 != 0 {
		t.Errorf("notes.txt came out %v, executable when the archive said otherwise", plain.Mode().Perm())
	}
}

// The entry cap bounds files, not the directories unpacking creates. A few
// thousand entries under long chains of parents are kilobytes to send and
// millions of directories to make.
func TestFetchSourceArchiveRefusesDeepNesting(t *testing.T) {
	deep := strings.Repeat("a/", maxArchiveDepth) + "model.py"
	archive := buildTestArchive(t, []archiveEntry{
		{name: manifestFile, body: codeManifest},
		{name: deep, body: testPySource},
	})

	_, cleanup, err := fetchSourceArchive(context.Background(), serveArchive(t, archive))
	if cleanup != nil {
		cleanup()
	}
	if err == nil {
		t.Fatal("a deeply nested entry was accepted")
	}
	if !strings.Contains(err.Error(), "nested deeper than") {
		t.Errorf("error = %v", err)
	}
}

func TestSafeArchivePathAllowsOrdinaryNesting(t *testing.T) {
	ok := strings.Repeat("a/", maxArchiveDepth-1) + "model.py"
	if _, err := safeArchivePath(ok); err != nil {
		t.Errorf("a path at the limit was refused: %v", err)
	}
}

// A TLS httptest server signs with a CA the client does not trust, so the rule
// is tested where it lives rather than through a redirect the fetch cannot make.
func TestArchiveClientRefusesAnHTTPSDowngrade(t *testing.T) {
	chain := func(schemes ...string) []*http.Request {
		var via []*http.Request
		for _, scheme := range schemes {
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, scheme+"://example.invalid/echo.zip", nil)
			if err != nil {
				t.Fatal(err)
			}
			via = append(via, req)
		}
		return via
	}

	cases := []struct {
		name    string
		to      string
		via     []*http.Request
		wantErr string
	}{
		{name: "https to http", to: "http", via: chain("https"), wantErr: "unencrypted"},
		{name: "https to https", to: "https", via: chain("https")},
		{name: "http to http", to: "http", via: chain("http")},
		{name: "too many", to: "https", via: chain(slices.Repeat([]string{"https"}, maxArchiveRedirects)...), wantErr: "redirects"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, tc.to+"://elsewhere.invalid/echo.zip", nil)
			if err != nil {
				t.Fatal(err)
			}
			err = archiveClient.CheckRedirect(req, tc.via)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("allowed redirect refused: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want one containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestFetchSourceArchiveRefusesTwoEntriesOnOnePath(t *testing.T) {
	for _, tc := range []struct {
		name    string
		entries []archiveEntry
		wantErr string
	}{
		{
			name: "the same path twice",
			entries: []archiveEntry{
				{name: testEntryFile, body: testPySource},
				{name: "./" + testEntryFile, body: "x = 2\n"},
			},
			wantErr: "twice",
		},
		{
			// One file on macOS and Windows, two on Linux, so the archive would
			// deploy different code depending on where it was unpacked.
			name: "two paths that differ only in case",
			entries: []archiveEntry{
				{name: testEntryFile, body: testPySource},
				{name: strings.ToUpper(testEntryFile[:1]) + testEntryFile[1:], body: "x = 2\n"},
			},
			wantErr: "case-insensitive",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			archive := buildTestArchive(t, append(
				[]archiveEntry{{name: manifestFile, body: codeManifest}},
				tc.entries...,
			))

			_, _, err := fetchSourceArchive(context.Background(), serveArchive(t, archive))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected a duplicate-path refusal naming %q, got %v", tc.wantErr, err)
			}
		})
	}
}
