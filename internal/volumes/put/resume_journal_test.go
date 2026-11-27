package put

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadSavedRejectsInvalidJournal(t *testing.T) {
	t.Parallel()
	type journalCase struct {
		name string
		data string
		want string
	}

	const session = `{"type":"session"}` + "\n"
	cases := []journalCase{
		{
			name: "empty",
			want: "no session record",
		},
		{
			name: "first record is not a session",
			data: `{"type":"publish"}` + "\n",
			want: "session record is not the first",
		},
		{
			name: "overlong line",
			data: session + strings.Repeat(" ", maxJournalLine+1) + "\n",
			want: "journal line is too long",
		},
		{
			name: "declared without file record",
			data: session + `{"type":"declared","index":3}` + "\n",
			want: "declared without a saved key",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, journalName), []byte(tc.data), 0o600); err != nil {
				t.Fatal(err)
			}
			if saved, err := loadSaved(dir); saved != nil || !errors.Is(err, errBadJournal) ||
				!strings.Contains(err.Error(), tc.want) {
				t.Fatalf("loadSaved returned state = %v, error = %v, want %q", saved != nil, err, tc.want)
			}
		})
	}
}
