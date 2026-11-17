package keypair

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestSequenceStoreRecordsAndRefusesLower: the record keeps the highest sequence per
// platform and organization, refuses a lower one and survives a new store on the same directory.
func TestSequenceStoreRecordsAndRefusesLower(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config")
	store := sequenceStore{dir: dir, host: testHost}
	accept := func(org [16]byte, sequence uint64) {
		t.Helper()
		if err := store.accept(org, sequence); err != nil {
			t.Fatalf("accept(%d): %v", sequence, err)
		}
	}

	accept(testOrg, 5)
	accept(testOrg, 5)
	if err := store.accept(testOrg, 4); !errors.Is(err, ErrStatement) {
		t.Fatalf("accept(4) after 5: error = %v, want ErrStatement", err)
	}
	if got := readMarks(t, dir)[orgKey(testOrg)]; got != 5 {
		t.Fatalf("a refused sequence moved the record to %d", got)
	}
	accept(testOrg, 6)
	accept(testOtherOrg, 1)

	fresh := sequenceStore{dir: dir, host: testHost}
	if err := fresh.accept(testOrg, 5); !errors.Is(err, ErrStatement) {
		t.Fatalf("a later run accepted 5 after 6: error = %v", err)
	}
	if err := fresh.accept(testOtherOrg, 0); !errors.Is(err, ErrStatement) {
		t.Fatalf("a later run accepted 0 after 1: error = %v", err)
	}
	want := map[string]uint64{
		orgKey(testOrg):      6,
		orgKey(testOtherOrg): 1,
	}
	if got := readMarks(t, dir); !maps.Equal(got, want) {
		t.Fatalf("recorded %v, want %v", got, want)
	}
}

// TestSequenceRefusalNamesTheRecordFile: a refused sequence says which file holds the mark.
func TestSequenceRefusalNamesTheRecordFile(t *testing.T) {
	store := sequenceStore{dir: t.TempDir(), host: testHost}
	if err := store.accept(testOrg, 5); err != nil {
		t.Fatal(err)
	}

	err := store.accept(testOrg, 4)

	if !errors.Is(err, ErrStatement) || !strings.Contains(err.Error(), store.path()) {
		t.Fatalf("error = %v, want ErrStatement naming %s", err, store.path())
	}
}

// TestSequenceRecordsArePerPlatform: an organization numbers its statements separately on each
// platform, so a high sequence on one never refuses a lower one on another.
func TestSequenceRecordsArePerPlatform(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "config")
	first := sequenceStore{dir: dir, host: testHost}
	second := sequenceStore{dir: dir, host: testOtherHost}

	if err := first.accept(testOrg, 5); err != nil {
		t.Fatal(err)
	}
	if err := second.accept(testOrg, 1); err != nil {
		t.Fatalf("a lower sequence on another platform: %v", err)
	}
	if err := first.accept(testOrg, 4); !errors.Is(err, ErrStatement) {
		t.Fatalf("accept(4) after 5 on the same platform: error = %v, want ErrStatement", err)
	}
	want := map[string]uint64{
		markKey(testHost, testOrg):      5,
		markKey(testOtherHost, testOrg): 1,
	}
	if got := readMarks(t, dir); !maps.Equal(got, want) {
		t.Fatalf("recorded %v, want %v", got, want)
	}
}

// TestSequenceConcurrentAcceptsKeepTheHighest: accepts racing in one process never lose an
// update, so the record ends at the highest sequence any of them was accepted at.
func TestSequenceConcurrentAcceptsKeepTheHighest(t *testing.T) {
	const (
		trials  = 8
		workers = 16
	)
	for range trials {
		dir := filepath.Join(t.TempDir(), "config")
		store := sequenceStore{dir: dir, host: testHost}
		if err := store.accept(testOrg, 1); err != nil {
			t.Fatal(err)
		}
		var (
			wg      sync.WaitGroup
			mu      sync.Mutex
			highest uint64
			start   = make(chan struct{})
		)
		for i := range workers {
			sequence := uint64(i) + 2
			wg.Go(func() {
				<-start
				err := store.accept(testOrg, sequence)
				switch {
				case err == nil:
					mu.Lock()
					highest = max(highest, sequence)
					mu.Unlock()
				case !errors.Is(err, ErrStatement):
					t.Errorf("accept(%d): %v", sequence, err)
				}
			})
		}
		close(start)
		wg.Wait()

		if got := readMarks(t, dir)[orgKey(testOrg)]; got != highest {
			t.Fatalf("the record ends at %d, but %d was accepted", got, highest)
		}
	}
}

// TestSequenceFileIsPrivate: the record is the only file, 0600, in a 0700 directory it creates,
// and a loose existing record is made private again by the next append.
func TestSequenceFileIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no POSIX file modes")
	}
	dir := filepath.Join(t.TempDir(), "config")
	store := sequenceStore{dir: dir, host: testHost}
	if err := store.accept(testOrg, 1); err != nil {
		t.Fatal(err)
	}
	assertPrivate := func() {
		t.Helper()
		dirInfo, err := os.Stat(dir)
		if err != nil {
			t.Fatal(err)
		}
		fileInfo, err := os.Stat(store.path())
		if err != nil {
			t.Fatal(err)
		}
		if dirInfo.Mode().Perm() != 0o700 || fileInfo.Mode().Perm() != 0o600 {
			t.Fatalf("directory mode %v and file mode %v, want 0700 and 0600", dirInfo.Mode().Perm(), fileInfo.Mode().Perm())
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Name() != sequenceFile {
			t.Fatalf("config directory holds %v, want only %s", entries, sequenceFile)
		}
	}
	assertPrivate()

	if err := os.Chmod(store.path(), 0o644); err != nil { //nolint:gosec // a loose record to be tightened
		t.Fatal(err)
	}
	if err := store.accept(testOrg, 2); err != nil {
		t.Fatal(err)
	}
	assertPrivate()
}

// TestSequenceStoreFailsClosed: a record that cannot be read or written is an error, never an
// accepted statement and never a reset of the record.
func TestSequenceStoreFailsClosed(t *testing.T) {
	t.Run("a record that does not parse", func(t *testing.T) {
		dir := t.TempDir()
		store := sequenceStore{dir: dir, host: testHost}
		garbage := []byte("{not json\n")
		if err := os.WriteFile(store.path(), garbage, 0o600); err != nil {
			t.Fatal(err)
		}
		err := store.accept(testOrg, 1)
		if err == nil || errors.Is(err, ErrStatement) || !strings.Contains(err.Error(), store.path()) {
			t.Fatalf("error = %v, want an error naming %s that is not ErrStatement", err, store.path())
		}
		if got, _ := os.ReadFile(store.path()); !bytes.Equal(got, garbage) {
			t.Fatal("the unreadable record was overwritten")
		}
	})
	t.Run("a record that is a directory", func(t *testing.T) {
		store := sequenceStore{dir: t.TempDir(), host: testHost}
		if err := os.Mkdir(store.path(), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := store.accept(testOrg, 1); err == nil || errors.Is(err, ErrStatement) {
			t.Fatalf("error = %v, want a read error that is not ErrStatement", err)
		}
	})
	t.Run("a record that is a symlink loop", func(t *testing.T) {
		store := sequenceStore{dir: t.TempDir(), host: testHost}
		if err := os.Symlink(store.path(), store.path()); err != nil {
			t.Skipf("cannot make a symlink loop: %v", err)
		}
		if err := store.accept(testOrg, 1); err == nil || errors.Is(err, ErrStatement) {
			t.Fatalf("error = %v, want a read error that is not ErrStatement", err)
		}
		if info, err := os.Lstat(store.path()); err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatal("the unreadable record was replaced")
		}
	})
	t.Run("a config directory that is a file", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "config")
		if err := os.WriteFile(dir, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := (sequenceStore{dir: dir, host: testHost}).accept(testOrg, 1); err == nil || errors.Is(err, ErrStatement) {
			t.Fatalf("error = %v, want an error that is not ErrStatement", err)
		}
	})
	t.Run("a config directory that cannot be created", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "config")
		if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), dir); err != nil {
			t.Skipf("cannot make a dangling symlink: %v", err)
		}
		err := (sequenceStore{dir: dir, host: testHost}).accept(testOrg, 1)
		if err == nil || errors.Is(err, ErrStatement) || !strings.Contains(err.Error(), "create the config directory") {
			t.Fatalf("error = %v, want a create-directory error that is not ErrStatement", err)
		}
	})
	t.Run("Verify returns no statement", func(t *testing.T) {
		v := loadVectors(t)
		verifier, dir := v.verifier(t, v.issuedAt.Add(time.Hour))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(verifier.seq.path(), []byte("{not json\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		st, err := verifier.Verify(v.statement, v.signature, v.org)
		if err == nil || errors.Is(err, ErrStatement) {
			t.Fatalf("error = %v, want a read error that is not ErrStatement", err)
		}
		if st.publicKey != nil {
			t.Fatal("Verify returned a statement it could not record")
		}
	})
}

// TestSequenceStoreSkipsAnUnfinishedLine: an empty record, or a final line another process has not
// finished appending, holds no mark and fails no read.
func TestSequenceStoreSkipsAnUnfinishedLine(t *testing.T) {
	t.Run("an empty record", func(t *testing.T) {
		dir := t.TempDir()
		store := sequenceStore{dir: dir, host: testHost}
		if err := os.WriteFile(store.path(), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := store.accept(testOrg, 1); err != nil {
			t.Fatal(err)
		}
		want := map[string]uint64{orgKey(testOrg): 1}
		if got := readMarks(t, dir); !maps.Equal(got, want) {
			t.Fatalf("recorded %v, want %v", got, want)
		}
	})
	t.Run("a final line without its newline", func(t *testing.T) {
		store := sequenceStore{dir: t.TempDir(), host: testHost}
		if err := store.accept(testOrg, 5); err != nil {
			t.Fatal(err)
		}
		appendRecord(t, store, markLine(t, 9))
		if err := store.accept(testOrg, 4); !errors.Is(err, ErrStatement) {
			t.Fatalf("accept(4) after 5: error = %v, want ErrStatement", err)
		}
		if err := store.accept(testOrg, 5); err != nil {
			t.Fatalf("accept(5) beside an unfinished 9: %v", err)
		}
	})
}

// TestSequenceMarkIsTheHighestLine: a writer that raced a higher sequence can append a lower one
// after it, and the higher still holds.
func TestSequenceMarkIsTheHighestLine(t *testing.T) {
	store := sequenceStore{dir: t.TempDir(), host: testHost}
	if err := store.accept(testOrg, 9); err != nil {
		t.Fatal(err)
	}
	appendRecord(t, store, append(markLine(t, 5), '\n'))

	if err := store.accept(testOrg, 6); !errors.Is(err, ErrStatement) {
		t.Fatalf("accept(6) after 9 then 5: error = %v, want ErrStatement", err)
	}
}

// TestSequenceStoreSkipsATornAppend: an append that stopped partway was never accepted, so the
// next accept leaves a readable record marked at its sequence, which still refuses a lower one.
func TestSequenceStoreSkipsATornAppend(t *testing.T) {
	torn := markLine(t, 9)
	for cut := 1; cut < len(torn); cut++ {
		store := sequenceStore{dir: t.TempDir(), host: testHost}
		if err := store.accept(testOrg, 5); err != nil {
			t.Fatal(err)
		}
		appendRecord(t, store, torn[:cut])

		if err := store.accept(testOrg, 6); err != nil {
			t.Fatalf("accept(6) after %q: %v", torn[:cut], err)
		}
		if mark, seen, err := store.mark(testOrg); err != nil || !seen || mark != 6 {
			t.Fatalf("after %q: mark = %d, %v, %v; want 6", torn[:cut], mark, seen, err)
		}
		if err := store.accept(testOrg, 5); !errors.Is(err, ErrStatement) {
			t.Fatalf("accept(5) after %q then 6: error = %v, want ErrStatement", torn[:cut], err)
		}
	}
}

// markLine is a record line for testOrg at sequence, without its newline.
func markLine(t *testing.T, sequence uint64) []byte {
	t.Helper()
	line, err := json.Marshal(sequenceMark{
		Host:         testHost,
		Organization: testOrg,
		Sequence:     sequence,
	})
	if err != nil {
		t.Fatal(err)
	}
	return line
}

// appendRecord writes data at the end of the record, as another process's append would.
func appendRecord(t *testing.T, store sequenceStore, data []byte) {
	t.Helper()
	f, err := os.OpenFile(store.path(), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Write(data)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
}

const sequenceChildEnv = "RUNWARE_TEST_SEQUENCE_CHILD_DIR"

// TestSequenceRecordHoldsAcrossProcesses: CLI processes accepting at once on one config directory
// never lower a mark or erase another organization's, so each mark ends at the highest sequence
// accepted for it.
func TestSequenceRecordHoldsAcrossProcesses(t *testing.T) {
	const (
		rounds   = 3
		children = 6
	)
	for range rounds {
		dir := filepath.Join(t.TempDir(), "config")
		if err := (sequenceStore{dir: dir, host: testHost}).accept(testOrg, 1); err != nil {
			t.Fatal(err)
		}
		// The highest sequence offered is never refused, so the shared mark must end there.
		want := map[string]uint64{orgKey(testOrg): children + 1}
		cmds := make([]*exec.Cmd, children)
		stdins := make([]io.WriteCloser, children)
		stdouts := make([]*bufio.Reader, children)
		for i := range children {
			own := uuid.New()
			want[orgKey(own)] = uint64(i) + 1
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSequenceAcceptChild$", //nolint:gosec // this test binary
				own.String(), strconv.Itoa(i+1),
				uuid.UUID(testOrg).String(), strconv.Itoa(i+2))
			// A race-enabled child otherwise sleeps a second before it exits.
			cmd.Env = append(os.Environ(), sequenceChildEnv+"="+dir, "GORACE=atexit_sleep_ms=0 "+os.Getenv("GORACE"))
			cmd.Stderr = os.Stderr
			stdin, err := cmd.StdinPipe()
			if err != nil {
				t.Fatal(err)
			}
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			cmds[i], stdins[i], stdouts[i] = cmd, stdin, bufio.NewReader(stdout)
		}
		for i, stdout := range stdouts {
			if line, err := stdout.ReadString('\n'); line != "ready\n" {
				t.Fatalf("child %d: read %q, %v", i, line, err)
			}
		}
		for _, stdin := range stdins {
			if err := stdin.Close(); err != nil {
				t.Fatal(err)
			}
		}
		for i, cmd := range cmds {
			out, err := io.ReadAll(stdouts[i])
			if err == nil {
				err = cmd.Wait()
			}
			if err != nil {
				t.Fatalf("child %d: %v\n%s", i, err, out)
			}
		}

		if got := readMarks(t, dir); !maps.Equal(got, want) {
			t.Fatalf("recorded %v, want %v", got, want)
		}
	}
}

// TestSequenceAcceptChild is a CLI process for TestSequenceRecordHoldsAcrossProcesses: once its
// stdin closes, it accepts each organization and sequence pair in its arguments.
func TestSequenceAcceptChild(t *testing.T) {
	dir := os.Getenv(sequenceChildEnv)
	if dir == "" {
		t.Skip("runs only as a child of TestSequenceRecordHoldsAcrossProcesses")
	}
	store := sequenceStore{dir: dir, host: testHost}
	var marks []sequenceMark
	for pair := range slices.Chunk(flag.Args(), 2) {
		sequence, err := strconv.ParseUint(pair[1], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		marks = append(marks, sequenceMark{
			Organization: uuid.MustParse(pair[0]),
			Sequence:     sequence,
		})
	}
	fmt.Println("ready")
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		t.Fatal(err)
	}
	for _, m := range marks {
		if err := store.accept(m.Organization, m.Sequence); err != nil && !errors.Is(err, ErrStatement) {
			t.Fatal(err)
		}
	}
}
