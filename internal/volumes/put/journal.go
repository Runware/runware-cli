package put

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	uploadsDirName = "uploads"
	journalName    = "journal.jsonl"
	journalVersion = 1
)

// Journal record types; session, file and declared records are synced before they are used.
const (
	recordSession  = "session"
	recordFile     = "file"
	recordDeclared = "declared"
	recordPart     = "part"
	recordAck      = "ack"
	recordComplete = "complete"
)

type sessionRecord struct {
	Type         string    `json:"type"`
	Version      int       `json:"version"`
	Upload       string    `json:"uploadId"`
	Volume       string    `json:"volume"`
	Environment  string    `json:"environment,omitempty"`
	Organization string    `json:"organization"`
	KeyPair      string    `json:"keyPairId"`
	SessionKey   []byte    `json:"sessionKey"`
	CreatedAt    time.Time `json:"createdAt"`
	ExpiresAt    time.Time `json:"expiresAt"`
}

type fileRecord struct {
	Type   string `json:"type"`
	Index  int    `json:"index"`
	Path   string `json:"path"`
	Mode   uint32 `json:"mode"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

type fileKeyRecord struct {
	DataKey   []byte `json:"dataKey"`
	SealedKey []byte `json:"sealedKey"`
	Attempt   string `json:"attemptId"`
}

type declaredRecord struct {
	Type   string      `json:"type"`
	Index  int         `json:"index"`
	FileID string      `json:"fileId"`
	State  string      `json:"state"`
	Layout *PartLayout `json:"layout,omitempty"`
}

type partRecord struct {
	Type   string `json:"type"`
	Index  int    `json:"index"`
	Part   int    `json:"part"`
	SHA256 string `json:"sha256"`
	Length int64  `json:"length"`
}

type ackRecord struct {
	Type      string `json:"type"`
	Index     int    `json:"index"`
	Part      int    `json:"part"`
	Ack       string `json:"ack"`
	CRC64NVME string `json:"crc64nvme"`
	Length    int64  `json:"length"`
}

type completeRecord struct {
	Type  string `json:"type"`
	Index int    `json:"index"`
}

// journal is the private resume state of one session, removed when the session completes or aborts.
type journal struct {
	mu   sync.Mutex
	dir  string
	file *os.File
}

// createJournal saves the session record, with the sealing key, before it returns.
func createJournal(stateDir string, rec sessionRecord) (*journal, error) {
	id, err := uuid.Parse(rec.Upload)
	if err != nil {
		return nil, fmt.Errorf("the session id is not a UUID: %w", err)
	}
	dir := filepath.Join(stateDir, uploadsDirName, id.String())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("create the resume state directory: %w", err)
	}
	rec.Type = recordSession
	rec.Version = journalVersion
	line, err := encodeLine(rec)
	if err != nil {
		return nil, err
	}
	if err := writeFileAtomic(dir, journalName, line); err != nil {
		return nil, fmt.Errorf("save the resume state: %w", err)
	}
	syncDir(dir)
	f, err := os.OpenFile(filepath.Join(dir, journalName), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open the resume state: %w", err)
	}
	return &journal{
		dir:  dir,
		file: f,
	}, nil
}

func encodeLine(rec any) ([]byte, error) {
	line, err := json.Marshal(rec)
	if err != nil {
		return nil, fmt.Errorf("encode a resume state record: %w", err)
	}
	return append(line, '\n'), nil
}

// log appends records without a sync.
func (j *journal) log(recs ...any) error { return j.write(false, recs) }

// logSync appends records and syncs them.
func (j *journal) logSync(recs ...any) error { return j.write(true, recs) }

func (j *journal) write(durable bool, recs []any) error {
	var batch []byte
	for _, rec := range recs {
		line, err := encodeLine(rec)
		if err != nil {
			return err
		}
		batch = append(batch, line...)
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.file == nil {
		return errors.New("write the resume state: the journal is closed")
	}
	if _, err := j.file.Write(batch); err != nil {
		return fmt.Errorf("write the resume state: %w", err)
	}
	if durable {
		if err := j.file.Sync(); err != nil {
			return fmt.Errorf("sync the resume state: %w", err)
		}
	}
	return nil
}

func fileKeyName(index int) string { return fmt.Sprintf("file-%d.key", index) }

func (j *journal) saveFileKeys(index int, rec fileKeyRecord) error {
	line, err := encodeLine(rec)
	if err != nil {
		return err
	}
	defer clear(line)
	if err := writeFileAtomic(j.dir, fileKeyName(index), line); err != nil {
		return fmt.Errorf("save the file's resume keys: %w", err)
	}
	return nil
}

func (j *journal) discardFileKeys(index int) error {
	if err := os.Remove(filepath.Join(j.dir, fileKeyName(index))); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove the file's resume keys: %w", err)
	}
	syncDir(j.dir)
	return nil
}

// close syncs and closes the journal without removing it.
func (j *journal) close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.file == nil {
		return nil
	}
	syncErr := j.file.Sync()
	return errors.Join(syncErr, j.closeLocked())
}

func (j *journal) closeLocked() error {
	if j.file == nil {
		return nil
	}
	err := j.file.Close()
	j.file = nil
	return err
}

// remove closes the journal and deletes its directory.
func (j *journal) remove() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	closeErr := j.closeLocked()
	if err := os.RemoveAll(j.dir); err != nil {
		return fmt.Errorf("remove the resume state: %w", err)
	}
	return closeErr
}

// syncDir flushes a directory entry where the platform allows it.
func syncDir(dir string) {
	d, err := os.Open(dir)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}
