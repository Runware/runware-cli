package serverless

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// fromFetchTimeout bounds the download of a packaged source.
const fromFetchTimeout = 60 * time.Second

// maxArchiveEntries bounds the file count in a fetched archive. The byte caps
// alone do not: a few hundred thousand empty entries cost almost nothing to
// send and a directory each to create.
const maxArchiveEntries = 2000

// maxArchiveDepth bounds how deep an entry may sit. The entry cap alone does
// not bound what unpacking creates: 2000 entries under distinct chains of a
// thousand parents each are a handful of kilobytes to send and two million
// directories to make.
const maxArchiveDepth = 32

// fetchSourceArchive downloads a packaged source and unpacks it into a new
// directory, returning that directory and a function that removes it.
//
// What comes back is read as code and uploaded to a builder, so the archive is
// treated as hostile: the caps bound what it can spend, and every entry is
// checked to land inside the directory as a regular file.
func fetchSourceArchive(ctx context.Context, rawURL string) (dir string, cleanup func(), err error) {
	archive, err := downloadArchive(ctx, rawURL)
	if err != nil {
		return "", nil, err
	}

	dir, err = os.MkdirTemp("", "runware-from-*")
	if err != nil {
		return "", nil, fmt.Errorf("create a directory for the fetched source: %w", err)
	}
	cleanup = func() {
		_ = os.RemoveAll(dir)
	}

	if err := unpackArchive(archive, dir); err != nil {
		cleanup()
		return "", nil, err
	}
	return dir, cleanup, nil
}

// maxArchiveRedirects is what the default client follows, kept so refusing a
// downgrade does not also change how far a redirect chain may go.
const maxArchiveRedirects = 10

// archiveClient refuses a redirect that leaves https for anything else. The
// scheme check in downloadArchive sees only the URL the person typed, and a
// client that follows redirects blindly would fetch the archive in the clear
// from a host the person never named.
var archiveClient = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxArchiveRedirects {
			return fmt.Errorf("stopped after %d redirects", maxArchiveRedirects)
		}
		if from := via[len(via)-1].URL.Scheme; from == "https" && req.URL.Scheme != "https" {
			return fmt.Errorf("the source redirected from https to %s, which would fetch the archive unencrypted", req.URL.Scheme)
		}
		return nil
	},
}

func httpScheme(scheme string) bool {
	return scheme == "http" || scheme == "https"
}

// downloadArchive reads the URL into memory, refusing anything larger than the
// platform would accept as a source upload anyway.
func downloadArchive(ctx context.Context, rawURL string) ([]byte, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("--from %q is not a URL: %w", rawURL, err)
	}
	if !httpScheme(parsed.Scheme) {
		return nil, fmt.Errorf("--from takes an http or https URL, not %q", rawURL)
	}

	ctx, cancel := context.WithTimeout(ctx, fromFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	resp, err := archiveClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	// Closed without being drained. The happy path reads the body to the end
	// anyway, and on every other path the body is one we have just refused:
	// reading the rest of it only gives a hostile source longer to send.
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch %s: the source answered %d", rawURL, resp.StatusCode)
	}

	archive, err := io.ReadAll(io.LimitReader(resp.Body, maxArchiveBytes+1))
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	if int64(len(archive)) > maxArchiveBytes {
		return nil, fmt.Errorf(
			"fetch %s: the archive is larger than %s, which is the most a source upload accepts",
			rawURL, humanBytes(maxArchiveBytes),
		)
	}
	return archive, nil
}

// unpackArchive writes the archive's files under dir.
func unpackArchive(archive []byte, dir string) error {
	reader, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return fmt.Errorf("read the fetched archive: %w", err)
	}
	if len(reader.File) > maxArchiveEntries {
		return fmt.Errorf("the fetched archive holds %d files, more than the %d a source may have", len(reader.File), maxArchiveEntries)
	}

	var total int64
	// Paths already written, keyed without case and holding the name they were
	// written under. unpackEntry opens each file with O_TRUNC and keeps no
	// record, so without this the second of two entries naming one path silently
	// replaces the first.
	//
	// Case is folded because `app.py` and `App.py` are one file on macOS and
	// Windows and two on Linux, so an archive carrying both deploys different
	// code depending on the machine that unpacks it. That refuses a pair Linux
	// would have handled, which is the price of one archive meaning one app.
	seen := make(map[string]string, len(reader.File))
	for _, entry := range reader.File {
		rel, err := safeArchivePath(entry.Name)
		if err != nil {
			return err
		}
		if !entry.FileInfo().IsDir() {
			key := strings.ToLower(rel)
			if first, dup := seen[key]; dup {
				if first == rel {
					return fmt.Errorf("the fetched archive writes %s twice", rel)
				}
				return fmt.Errorf("the fetched archive writes both %s and %s, which are one file on a case-insensitive filesystem", first, rel)
			}
			seen[key] = rel
		}

		written, err := unpackEntry(entry, dir)
		if err != nil {
			return err
		}
		total += written
		if total > maxPackTotalBytes {
			return fmt.Errorf("the fetched archive unpacks to more than %s", humanBytes(maxPackTotalBytes))
		}
	}
	return nil
}

// unpackEntry writes one archive entry and reports the bytes it wrote.
func unpackEntry(entry *zip.File, dir string) (int64, error) {
	rel, err := safeArchivePath(entry.Name)
	if err != nil {
		return 0, err
	}
	path := filepath.Join(dir, rel)

	if entry.FileInfo().IsDir() {
		if err := os.MkdirAll(path, 0o750); err != nil {
			return 0, fmt.Errorf("create %s: %w", entry.Name, err)
		}
		return 0, nil
	}

	// A symlink in the archive would be followed by the packer that reads this
	// directory back, and could name any file on the machine running the deploy.
	if mode := entry.Mode(); !mode.IsRegular() {
		return 0, fmt.Errorf("%s in the fetched archive is not a regular file", entry.Name)
	}
	if entry.UncompressedSize64 > uint64(maxPackEntryBytes) {
		return 0, fmt.Errorf("%s in the fetched archive is larger than %s", entry.Name, humanBytes(maxPackEntryBytes))
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return 0, fmt.Errorf("create a directory for %s: %w", entry.Name, err)
	}

	source, err := entry.Open()
	if err != nil {
		return 0, fmt.Errorf("read %s from the fetched archive: %w", entry.Name, err)
	}
	defer func() {
		_ = source.Close()
	}()

	// Owner-only, since this directory is ours, but the executable bit is kept:
	// the packer that reads the directory back archives an executable entry as
	// 0755, and a container whose entrypoint script lost it would not start.
	mode := os.FileMode(0o600)
	if entry.Mode().Perm()&0o111 != 0 {
		mode = 0o700
	}

	dest, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode) //nolint:gosec // safeArchivePath keeps path under dir
	if err != nil {
		return 0, fmt.Errorf("write %s: %w", entry.Name, err)
	}

	// One byte past the cap, so a lying UncompressedSize64 is caught by the copy
	// rather than trusted.
	written, err := io.Copy(dest, io.LimitReader(source, maxPackEntryBytes+1))
	if closeErr := dest.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return 0, fmt.Errorf("write %s: %w", entry.Name, err)
	}
	if written > maxPackEntryBytes {
		return 0, fmt.Errorf("%s in the fetched archive is larger than %s", entry.Name, humanBytes(maxPackEntryBytes))
	}
	return written, nil
}

// safeArchivePath turns an entry name into a relative path under the
// destination, refusing one that would write outside it.
func safeArchivePath(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("the fetched archive holds an entry with no name")
	}
	// Zip names are slash-separated by specification, but a Windows-made archive
	// may still carry backslashes, which are an ordinary character in a POSIX
	// filename and would hide a traversal from the check below.
	if strings.ContainsRune(name, '\\') {
		return "", fmt.Errorf("%q in the fetched archive is not a valid path", name)
	}
	if strings.HasPrefix(name, "/") || filepath.IsAbs(filepath.FromSlash(name)) {
		return "", fmt.Errorf("%q in the fetched archive is an absolute path", name)
	}

	clean := filepath.Clean(filepath.FromSlash(name))

	// IsLocal rather than a prefix test: on Windows a drive-relative entry such
	// as `C:../../../escaped.py` is not absolute and does not begin with "..",
	// so it passes a hand-written check and then climbs out of the directory
	// once joined. It also refuses the reserved device names.
	if !filepath.IsLocal(clean) {
		return "", fmt.Errorf("%q in the fetched archive points outside it", name)
	}
	if depth := len(strings.Split(filepath.ToSlash(clean), "/")); depth > maxArchiveDepth {
		return "", fmt.Errorf("%q in the fetched archive is nested deeper than %d directories", name, maxArchiveDepth)
	}
	return clean, nil
}
