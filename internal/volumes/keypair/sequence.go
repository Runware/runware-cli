package keypair

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

const sequenceFile = "volumes-statement-sequence.jsonl"

// sequenceMark is one line of the sequence record.
type sequenceMark struct {
	Host         string    `json:"host"`
	Organization uuid.UUID `json:"organization"`
	Sequence     uint64    `json:"sequence"`
}

// sequenceStore holds, per platform host and organization, the highest statement sequence this CLI
// accepted as the highest line of an append-only record, so no concurrent writer can lower a mark.
type sequenceStore struct {
	dir  string
	host string
}

func (s sequenceStore) path() string { return filepath.Join(s.dir, sequenceFile) }

// accept refuses a sequence below the one recorded for the platform and organization, then records it.
func (s sequenceStore) accept(organization [16]byte, sequence uint64) error {
	org := uuid.UUID(organization)
	mark, seen, err := s.mark(org)
	if err != nil {
		return err
	}
	if seen && sequence < mark {
		return refuse("sequence %d is below the highest accepted, %d (recorded in %s)", sequence, mark, s.path())
	}
	if seen && sequence == mark {
		return nil
	}
	return s.record(sequenceMark{
		Host:         s.host,
		Organization: org,
		Sequence:     sequence,
	})
}

func (s sequenceStore) mark(organization uuid.UUID) (mark uint64, seen bool, err error) {
	data, err := os.ReadFile(s.path())
	if errors.Is(err, fs.ErrNotExist) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("read the statement sequence record: %w", err)
	}
	// A final line without its newline is an append still being written.
	for line := range bytes.Lines(data[:bytes.LastIndexByte(data, '\n')+1]) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var m sequenceMark
		if err = json.Unmarshal(line, &m); err != nil {
			// A line that ends mid-value is an append that stopped partway, so it was never accepted.
			if errors.Is(json.NewDecoder(bytes.NewReader(line)).Decode(new(json.RawMessage)), io.ErrUnexpectedEOF) {
				continue
			}
			return 0, false, fmt.Errorf("parse the statement sequence record %s: %w", s.path(), err)
		}
		if m.Host == s.host && m.Organization == organization && (!seen || m.Sequence > mark) {
			mark, seen = m.Sequence, true
		}
	}
	return mark, seen, nil
}

func (s sequenceStore) record(line sequenceMark) error {
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("create the config directory: %w", err)
	}
	data, err := json.Marshal(line)
	if err != nil {
		return fmt.Errorf("write the statement sequence record: %w", err)
	}
	f, err := os.OpenFile(s.path(), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("write the statement sequence record: %w", err)
	}
	err = f.Chmod(0o600)
	if err == nil {
		// One write per line so appends never interleave, led by a newline that ends any torn append.
		_, err = f.WriteString("\n" + string(data) + "\n")
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write the statement sequence record: %w", err)
	}
	return nil
}
