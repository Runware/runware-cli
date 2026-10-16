package installtest

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOnPath checks that a tool counts as found only when sh could run it
// from the PATH the script gets.
func TestOnPath(t *testing.T) {
	empty := t.TempDir()
	tools := t.TempDir()
	if err := os.WriteFile(filepath.Join(tools, "curl"), []byte("#!/bin/sh\n"), 0o700); err != nil { //nolint:gosec // test executable
		t.Fatalf("write curl: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tools, "tar"), []byte("data\n"), 0o600); err != nil {
		t.Fatalf("write tar: %v", err)
	}
	if err := os.Mkdir(filepath.Join(tools, "shasum"), 0o700); err != nil {
		t.Fatalf("mkdir shasum: %v", err)
	}
	path := empty + string(os.PathListSeparator) + tools

	tests := []struct {
		name string
		want bool
	}{
		{
			name: "curl",
			want: true,
		},
		{
			name: "tar",
			want: false,
		},
		{
			name: "shasum",
			want: false,
		},
		{
			name: "missing",
			want: false,
		},
	}
	for _, tt := range tests {
		if got := onPath(tt.name, path); got != tt.want {
			t.Errorf("onPath(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
