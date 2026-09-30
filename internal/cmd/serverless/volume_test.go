package serverless

import (
	"strings"
	"testing"
)

// A representative mount path, repeated across the cases below.
const testMountPath = "/data/weights"

func TestBuildVolumes(t *testing.T) {
	got, err := buildVolumes([]string{"/root/.cache/huggingface", testMountPath})
	if err != nil {
		t.Fatalf("buildVolumes: %v", err)
	}
	if got == nil || len(*got) != 2 {
		t.Fatalf("expected 2 volumes, got %v", got)
	}
	if (*got)[0].MountPath != "/root/.cache/huggingface" {
		t.Errorf("mountPath = %q", (*got)[0].MountPath)
	}
}

// No volumes must send nil rather than an empty array: the field is optional and
// an explicit [] is a different statement from saying nothing.
func TestBuildVolumes_NoneIsNil(t *testing.T) {
	got, err := buildVolumes(nil)
	if err != nil {
		t.Fatalf("buildVolumes: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for no volumes, got %v", *got)
	}
}

// The path is cleaned before it travels, so the server sees one spelling of a
// path however it was typed -- and so the overlap check below compares like
// with like.
func TestBuildVolumes_CleansPaths(t *testing.T) {
	got, err := buildVolumes([]string{"/data//weights/"})
	if err != nil {
		t.Fatalf("buildVolumes: %v", err)
	}
	if (*got)[0].MountPath != testMountPath {
		t.Errorf("mountPath = %q, want /data/weights", (*got)[0].MountPath)
	}
}

func TestBuildVolumes_Rejects(t *testing.T) {
	cases := []struct {
		name  string
		paths []string
		want  string
	}{
		{
			name:  "relative",
			paths: []string{"cache/weights"},
			want:  "absolute",
		},
		{
			name:  "root",
			paths: []string{"/"},
			want:  "root directory",
		},
		{
			name:  "unsupported character",
			paths: []string{"/data/we ights"},
			want:  "unsupported character",
		},
		{
			// The same path twice is a mistake, not a request for one volume.
			name:  "duplicate",
			paths: []string{testMountPath, testMountPath},
			want:  "listed twice",
		},
		{
			// The pair that motivates the whole check: one contains the other,
			// so the same node directory would be mounted into the sandbox twice.
			name:  "overlapping",
			paths: []string{"/data", testMountPath},
			want:  "overlaps",
		},
		{
			// ...in either order.
			name:  "overlapping reversed",
			paths: []string{testMountPath, "/data"},
			want:  "overlaps",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildVolumes(tc.paths)
			if err == nil {
				t.Fatalf("expected an error for %v", tc.paths)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

// A shared prefix is not containment: /data/weights-old sits beside
// /data/weights rather than inside it, so both are legitimate.
func TestBuildVolumes_SharedPrefixIsNotOverlap(t *testing.T) {
	got, err := buildVolumes([]string{testMountPath, "/data/weights-old"})
	if err != nil {
		t.Fatalf("buildVolumes rejected sibling paths: %v", err)
	}
	if len(*got) != 2 {
		t.Errorf("expected both volumes, got %v", *got)
	}
}

func TestBuildVolumes_TooMany(t *testing.T) {
	paths := make([]string, maxVolumes+1)
	for i := range paths {
		paths[i] = "/data/vol" + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	_, err := buildVolumes(paths)
	if err == nil {
		t.Fatal("expected an error past the volume limit")
	}
	if !strings.Contains(err.Error(), "at most") {
		t.Errorf("error %q does not mention the limit", err)
	}
}
