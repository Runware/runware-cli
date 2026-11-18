package put

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/runware/runware-cli/pkg/volumeformat"
)

// lockedBuffer collects what the engine writes to its progress writer.
type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// testEnv is one organization's cloud, keys, config directory and source directory.
type testEnv struct {
	t         *testing.T
	vec       vectorKeys
	cloud     *fakeCloud
	keys      *fakeKeys
	configDir string
	srcDir    string
	progress  *lockedBuffer
	skew      atomic.Int64

	mu     sync.Mutex
	hashed []string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	vec := loadVectorKeys(t)
	return &testEnv{
		t:         t,
		vec:       vec,
		cloud:     newFakeCloud(t, vec),
		keys:      &fakeKeys{vec: vec},
		configDir: filepath.Join(t.TempDir(), "config"),
		srcDir:    t.TempDir(),
		progress:  &lockedBuffer{},
	}
}

func (e *testEnv) org() string { return uuid.UUID(e.vec.org).String() }

// options wires a put of paths into vol against the fake cloud, with no sleeping and a skewable clock.
func (e *testEnv) options(vol VolumeRef, paths ...string) Options {
	return Options{
		Volume:       vol,
		Organization: e.org(),
		Paths:        paths,
		ConfigDir:    e.configDir,
		API:          e.cloud,
		Keys:         e.keys,
		Transport:    NewHTTPTransport(e.cloud.server.Client()),
		Parallelism:  2,
		Progress:     e.progress,
		now:          func() time.Time { return time.Now().Add(time.Duration(e.skew.Load())) },
		sleep:        func(ctx context.Context, _ time.Duration) error { return ctx.Err() },
		onHash: func(p string) {
			e.mu.Lock()
			defer e.mu.Unlock()
			e.hashed = append(e.hashed, p)
		},
	}
}

func (e *testEnv) put(vol VolumeRef, paths ...string) (Summary, error) {
	return Run(context.Background(), e.options(vol, paths...))
}

func (e *testEnv) mustPut(vol VolumeRef, paths ...string) Summary {
	e.t.Helper()
	sum, err := e.put(vol, paths...)
	if err != nil {
		e.t.Fatalf("put: %v", err)
	}
	return sum
}

func (e *testEnv) hashedCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.hashed)
}

func (e *testEnv) stateDir() string { return filepath.Join(e.configDir, stateDirName) }

// uploadDirs lists the resume state directories that exist now.
func (e *testEnv) uploadDirs() []string {
	entries, err := os.ReadDir(filepath.Join(e.stateDir(), uploadsDirName))
	if err != nil {
		return nil
	}
	var dirs []string
	for _, entry := range entries {
		dirs = append(dirs, filepath.Join(e.stateDir(), uploadsDirName, entry.Name()))
	}
	return dirs
}

// patterned returns n deterministic bytes that differ per seed.
func patterned(n int, seed byte) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i%251) + seed
	}
	return b
}

func writeFile(t *testing.T, p string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, content, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeTree(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for rel, content := range files {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(rel)), content)
	}
}

// rewrite replaces a file's content and moves its modification time on.
func rewrite(t *testing.T, p string, content []byte) {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, p, content)
	later := info.ModTime().Add(2 * time.Second)
	if err := os.Chtimes(p, later, later); err != nil {
		t.Fatal(err)
	}
}

// twoPartSize is the smallest plaintext that takes two parts.
const twoPartSize = int64(volumeformat.SegmentPlaintextBytes)*volumeformat.MinSegmentsPerPart + 1000

// writeTwoPartFile makes a file of two parts, mostly empty, with marks in both parts.
func writeTwoPartFile(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(twoPartSize); err != nil {
		t.Fatal(err)
	}
	firstPartSize := int64(volumeformat.SegmentPlaintextBytes) * volumeformat.MinSegmentsPerPart
	for _, off := range []int64{0, firstPartSize - 100, firstPartSize, twoPartSize - 100} {
		if _, err := f.WriteAt(patterned(100, byte(off%200)), off); err != nil {
			t.Fatal(err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// expectedFiles reads a directory as the Volume's files it should become under prefix.
func expectedFiles(t *testing.T, root, prefix string) []VolumeFile {
	t.Helper()
	var files []VolumeFile
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		sum, err := fileSHA256(p)
		if err != nil {
			return err
		}
		files = append(files, VolumeFile{
			Path:   path.Join(prefix, filepath.ToSlash(rel)),
			Mode:   uint32(info.Mode().Perm() & modeMask),
			Size:   info.Size(),
			SHA256: sum,
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(files, func(a, b VolumeFile) int { return strings.Compare(a.Path, b.Path) })
	return files
}

func fileSHA256(p string) (string, error) {
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ciphertextSize is the object size of a file of size plaintext bytes.
func ciphertextSize(t *testing.T, size int64) int64 {
	t.Helper()
	l, err := volumeformat.PlanLayout(size)
	if err != nil {
		t.Fatal(err)
	}
	return l.CiphertextSize
}

func totalCiphertext(t *testing.T, files []VolumeFile) int64 {
	t.Helper()
	var n int64
	for i := range files {
		n += ciphertextSize(t, files[i].Size)
	}
	return n
}

// journalSnapshot is the resume state of the one open session, read while it is live.
type journalSnapshot struct {
	Dir     string
	Records []map[string]any
	DirMode fs.FileMode
	Mode    fs.FileMode
}

func (s journalSnapshot) ofType(typ string) []map[string]any {
	var out []map[string]any
	for _, r := range s.Records {
		if r["type"] == typ {
			out = append(out, r)
		}
	}
	return out
}

// readJournal reads the only resume state directory under configDir.
func readJournal(t *testing.T, configDir string) journalSnapshot {
	t.Helper()
	root := filepath.Join(configDir, stateDirName, uploadsDirName)
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 1 {
		t.Errorf("want one resume state directory under %s, found %d (%v)", root, len(entries), err)
		return journalSnapshot{}
	}
	dir := filepath.Join(root, entries[0].Name())
	file, err := os.Open(filepath.Join(dir, journalName))
	if err != nil {
		t.Errorf("open the journal: %v", err)
		return journalSnapshot{}
	}
	defer func() { _ = file.Close() }()
	snap := journalSnapshot{Dir: dir}
	if info, err := os.Stat(dir); err == nil {
		snap.DirMode = info.Mode().Perm()
	}
	if info, err := file.Stat(); err == nil {
		snap.Mode = info.Mode().Perm()
	}
	sc := bufio.NewScanner(file)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		var rec map[string]any
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			t.Errorf("a journal line is not JSON: %v", err)
			continue
		}
		snap.Records = append(snap.Records, rec)
	}
	return snap
}
