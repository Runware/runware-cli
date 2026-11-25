package put

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/google/uuid"
	"github.com/runware/runware-cli/pkg/volumeformat"
)

// goosWindows is the platform without POSIX file modes.
const goosWindows = "windows"

// maxJournalLine bounds one journal line; the longest record, a file record, is far below it.
const maxJournalLine = 64 << 10

// errBadJournal is resume state that cannot be read back.
var errBadJournal = errors.New("the resume state cannot be read")

// savedSession is one session's resume state as its journal holds it.
type savedSession struct {
	dir        string
	lock       *os.File
	session    sessionRecord
	files      map[int]*savedFile // by journal index
	order      []int              // journal indexes in the order they were saved
	publishing bool
	actions    map[int]string
	// validLen is the length of the journal's complete lines; a torn tail after it is cut on reopen.
	validLen int64
}

func (s *savedSession) release() {
	clear(s.session.SessionKey)
	if s.lock != nil {
		_ = s.lock.Close()
		s.lock = nil
	}
}

// savedFile is one journalled file: its key, its answer, and the digests and acknowledgements of its parts.
type savedFile struct {
	rec      fileRecord
	declared *declaredRecord
	digests  map[int]string // part number to the plaintext SHA-256 read last
	acks     map[int]ackRecord
	complete bool
}

type recordType struct {
	Type string `json:"type"`
}

// loadSaved reads a session's journal. A last line without its newline is torn, from a crash during a
// write, and is dropped; every complete line must read back.
func loadSaved(dir string) (*savedSession, error) {
	data, err := os.ReadFile(filepath.Join(dir, journalName))
	if err != nil {
		return nil, fmt.Errorf("%w: %w", errBadJournal, err)
	}
	defer clear(data)
	s := &savedSession{
		dir:     dir,
		files:   map[int]*savedFile{},
		actions: map[int]string{},
	}
	s.validLen = int64(bytes.LastIndexByte(data, '\n') + 1)
	lines := bytes.Split(data[:s.validLen], []byte("\n"))
	lines = lines[:len(lines)-1] // the empty string after the last newline
	if len(lines) == 0 {
		return nil, fmt.Errorf("%w: the journal holds no session record", errBadJournal)
	}
	for i, line := range lines {
		if len(line) > maxJournalLine {
			return nil, fmt.Errorf("%w: a journal line is too long", errBadJournal)
		}
		if err := s.apply(line, i == 0); err != nil {
			return nil, err
		}
	}
	return s, nil
}

func (s *savedSession) apply(line []byte, first bool) error {
	var t recordType
	if err := json.Unmarshal(line, &t); err != nil {
		return fmt.Errorf("%w: %w", errBadJournal, err)
	}
	if first != (t.Type == recordSession) {
		return fmt.Errorf("%w: the session record is not the first", errBadJournal)
	}
	var err error
	switch t.Type {
	case recordSession:
		err = json.Unmarshal(line, &s.session)
	case recordFile:
		var rec fileRecord
		if err = json.Unmarshal(line, &rec); err == nil {
			if _, dup := s.files[rec.Index]; dup {
				return fmt.Errorf("%w: file %d is saved twice", errBadJournal, rec.Index)
			}
			s.files[rec.Index] = &savedFile{
				rec:     rec,
				digests: map[int]string{},
				acks:    map[int]ackRecord{},
			}
			s.order = append(s.order, rec.Index)
		}
	case recordDeclared:
		var rec declaredRecord
		if err = json.Unmarshal(line, &rec); err == nil {
			f, ok := s.files[rec.Index]
			if !ok {
				// A declared file without its key: the session lost a key and cannot resume.
				return fmt.Errorf("%w: file %d was declared without a saved key", errBadJournal, rec.Index)
			}
			f.declared = &rec
		}
	case recordPart:
		var rec partRecord
		if err = json.Unmarshal(line, &rec); err == nil {
			if f, ok := s.files[rec.Index]; ok {
				f.digests[rec.Part] = rec.SHA256
			}
		}
	case recordAck:
		var rec ackRecord
		if err = json.Unmarshal(line, &rec); err == nil {
			if f, ok := s.files[rec.Index]; ok {
				f.acks[rec.Part] = rec
			}
		}
	case recordComplete:
		var rec completeRecord
		if err = json.Unmarshal(line, &rec); err == nil {
			if f, ok := s.files[rec.Index]; ok {
				f.complete = true
			}
		}
	case recordAction:
		var rec actionRecord
		if err = json.Unmarshal(line, &rec); err == nil {
			s.actions[rec.Index] = rec.Action
		}
	case recordPublish:
		s.publishing = true
	default:
		return fmt.Errorf("%w: unknown record %q", errBadJournal, t.Type)
	}
	if err != nil {
		return fmt.Errorf("%w: %w", errBadJournal, err)
	}
	return nil
}

// reopenJournal cuts a torn last line off a saved journal, opens it for appending and makes its directory
// 0700 and its files 0600 again, in case a copy or a restore widened them.
func reopenJournal(s *savedSession) (*journal, error) {
	path := filepath.Join(s.dir, journalName)
	if runtime.GOOS != goosWindows {
		if err := os.Chmod(s.dir, 0o700); err != nil { //nolint:gosec // a directory needs its execute bit
			return nil, fmt.Errorf("protect the resume state: %w", err)
		}
		entries, err := os.ReadDir(s.dir)
		if err != nil {
			return nil, fmt.Errorf("protect the resume state: %w", err)
		}
		for _, e := range entries {
			if e.Type().IsRegular() {
				if err := os.Chmod(filepath.Join(s.dir, e.Name()), 0o600); err != nil {
					return nil, fmt.Errorf("protect the resume state: %w", err)
				}
			}
		}
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open the resume state: %w", err)
	}
	if err := f.Truncate(s.validLen); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("write the resume state: %w", err)
	}
	if _, err := f.Seek(s.validLen, 0); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("seek in the resume state: %w", err)
	}
	return &journal{
		dir:  s.dir,
		file: f,
		lock: s.lock,
	}, nil
}

// storedFileKey is a file's data key read back from the resume state; it prints as [redacted].
type storedFileKey struct {
	dataKey  []byte
	sealed   []byte
	identity volumeformat.FileIdentity
}

var _ FileKey = (*storedFileKey)(nil)

func (k *storedFileKey) DataKey() []byte                     { return k.dataKey }
func (k *storedFileKey) Attempt() [16]byte                   { return k.identity.Attempt }
func (k *storedFileKey) Sealed() []byte                      { return k.sealed }
func (k *storedFileKey) Identity() volumeformat.FileIdentity { return k.identity }
func (k *storedFileKey) Clear()                              { clear(k.dataKey) }
func (k *storedFileKey) String() string                      { return redactedText }
func (k *storedFileKey) GoString() string                    { return redactedText }
func (k *storedFileKey) LogValue() slog.Value                { return slog.StringValue(redactedText) }

// loadFileKeys reads a saved file's keys; a file that completed or reused content has none.
func loadFileKeys(dir string, index int) (fileKeyRecord, bool, error) {
	data, err := os.ReadFile(filepath.Join(dir, fileKeyName(index)))
	if errors.Is(err, os.ErrNotExist) {
		return fileKeyRecord{}, false, nil
	}
	if err != nil {
		return fileKeyRecord{}, false, fmt.Errorf("%w: %w", errBadJournal, err)
	}
	defer clear(data)
	var keys fileKeyRecord
	if err := json.Unmarshal(data, &keys); err != nil {
		return fileKeyRecord{}, false, fmt.Errorf("%w: the keys of file %d: %w", errBadJournal, index, err)
	}
	return keys, true, nil
}

// storedKey rebuilds a saved file's key and checks it against the session sealing key: a key that does not
// unwrap from its sealed form, under the identity it was declared with, is lost.
func storedKey(sessionKey []byte, session, org [16]byte, rec fileRecord, keys fileKeyRecord) (*storedFileKey, error) {
	sum, err := hex.DecodeString(rec.SHA256)
	if err != nil || len(sum) != 32 {
		return nil, fmt.Errorf("%w: file %d has no valid digest", errBadJournal, rec.Index)
	}
	attempt, err := hex.DecodeString(keys.Attempt)
	if err != nil || len(attempt) != 16 {
		return nil, fmt.Errorf("%w: file %d has no valid attempt id", errBadJournal, rec.Index)
	}
	if rec.Size < 0 {
		return nil, fmt.Errorf("%w: file %d has a negative size", errBadJournal, rec.Index)
	}
	id := volumeformat.FileIdentity{
		Organization: org,
		SHA256:       [32]byte(sum),
		Size:         uint64(rec.Size),
		Attempt:      [16]byte(attempt),
	}
	got, err := volumeformat.UnwrapDataKey(sessionKey, session, id, keys.SealedKey)
	if err != nil {
		return nil, fmt.Errorf("%w: the key of file %d does not open: %w", errBadJournal, rec.Index, err)
	}
	same := bytes.Equal(got, keys.DataKey)
	clear(got)
	if !same {
		return nil, fmt.Errorf("%w: the key of file %d is not the one it was sealed with", errBadJournal, rec.Index)
	}
	return &storedFileKey{
		dataKey:  bytes.Clone(keys.DataKey),
		sealed:   bytes.Clone(keys.SealedKey),
		identity: id,
	}, nil
}

// sealStoredFile draws a fresh data key and attempt for a file a resumed session has not saved yet,
// and seals it under the stored session key, as the key-pair session would.
func sealStoredFile(sessionKey []byte, session, org [16]byte, digest [32]byte, size uint64) (*storedFileKey, error) {
	id := volumeformat.FileIdentity{
		Organization: org,
		SHA256:       digest,
		Size:         size,
		Attempt:      volumeformat.NewAttemptID(),
	}
	dataKey := volumeformat.NewDataKey()
	sealed, err := volumeformat.WrapDataKey(sessionKey, session, id, dataKey)
	if err != nil {
		clear(dataKey)
		return nil, err
	}
	return &storedFileKey{
		dataKey:  dataKey,
		sealed:   sealed,
		identity: id,
	}, nil
}

// trustedAck returns a part's saved acknowledgement when it can stand for the part: its digest was
// journalled before it, and its length is the part's length in the layout.
func (f *savedFile) trustedAck(l volumeformat.Layout, part int) (partResult, bool) {
	a, ok := f.acks[part]
	if !ok || a.Ack == "" {
		return partResult{}, false
	}
	if _, ok := f.digests[part]; !ok {
		return partResult{}, false
	}
	want, err := l.PartLength(int64(part))
	if err != nil || a.Length != want {
		return partResult{}, false
	}
	crc, err := strconv.ParseUint(a.CRC64NVME, 16, 64)
	if err != nil {
		return partResult{}, false
	}
	return partResult{
		Ack: Secret(a.Ack),
		CRC: crc,
		Len: a.Length,
	}, true
}

// sessionID is the saved session's id.
func (s *savedSession) sessionID() (uuid.UUID, error) {
	id, err := uuid.Parse(s.session.Upload)
	if err != nil || id.String() != filepath.Base(s.dir) {
		return uuid.UUID{}, fmt.Errorf("%w: the session id does not name its directory", errBadJournal)
	}
	return id, nil
}

// keyless reports whether a saved file no longer needs its keys: it completed, or its content was reused.
func (f *savedFile) keyless() bool {
	return f.complete || (f.declared != nil && (f.declared.State == stateReused || f.declared.State == stateVerified))
}
