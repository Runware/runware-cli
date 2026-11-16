package keypair

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
)

const sequenceFile = "volumes-statement-sequence.json"

// recordMu serializes accept within this process; another process is not excluded.
var recordMu sync.Mutex

// sequenceStore holds, per platform host and organization, the highest statement sequence this CLI accepted.
type sequenceStore struct {
	dir  string
	host string
}

func (s sequenceStore) path() string { return filepath.Join(s.dir, sequenceFile) }

// accept refuses a sequence below the one recorded for the platform and organization, then records it.
func (s sequenceStore) accept(organization [16]byte, sequence uint64) error {
	recordMu.Lock()
	defer recordMu.Unlock()
	marks, err := s.load()
	if err != nil {
		return err
	}
	id := s.host + "/" + uuid.UUID(organization).String()
	mark, seen := marks[id]
	if seen && sequence < mark {
		return refuse("sequence %d is below the highest accepted, %d (recorded in %s)", sequence, mark, s.path())
	}
	if seen && sequence == mark {
		return nil
	}
	marks[id] = sequence
	return s.save(marks)
}

func (s sequenceStore) load() (map[string]uint64, error) {
	data, err := os.ReadFile(s.path())
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]uint64{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the statement sequence record: %w", err)
	}
	var marks map[string]uint64
	if err := json.Unmarshal(data, &marks); err != nil {
		return nil, fmt.Errorf("parse the statement sequence record %s: %w", s.path(), err)
	}
	if marks == nil {
		marks = map[string]uint64{}
	}
	return marks, nil
}

func (s sequenceStore) save(marks map[string]uint64) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create the config directory: %w", err)
	}
	tmp, err := os.CreateTemp(s.dir, sequenceFile+".*")
	if err != nil {
		return fmt.Errorf("write the statement sequence record: %w", err)
	}
	err = json.NewEncoder(tmp).Encode(marks)
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), s.path())
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
		return fmt.Errorf("write the statement sequence record: %w", err)
	}
	return nil
}
