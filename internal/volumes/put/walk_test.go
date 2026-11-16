package put

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func targets(sources []Source) []string {
	out := make([]string, 0, len(sources))
	for i := range sources {
		out = append(out, sources[i].Target)
	}
	return out
}

func symlinkOrSkip(t *testing.T, oldname, newname string) {
	t.Helper()
	if err := os.Symlink(oldname, newname); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}
}

func TestWalkDirectoryKeepsRelativePaths(t *testing.T) {
	root := filepath.Join(t.TempDir(), "weights")
	writeTree(t, root, map[string][]byte{
		"a.bin":        patterned(3, 1),
		"sub/b.bin":    patterned(4, 2),
		"sub/dir/c.md": patterned(5, 3),
	})

	got, err := Walk([]string{root}, "")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a.bin", "sub/b.bin", "sub/dir/c.md"}; !slices.Equal(targets(got), want) {
		t.Errorf("targets = %v, want %v", targets(got), want)
	}

	under, err := Walk([]string{root}, "/models/x/")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"models/x/a.bin", "models/x/sub/b.bin", "models/x/sub/dir/c.md"}; !slices.Equal(targets(under), want) {
		t.Errorf("targets under --to = %v, want %v", targets(under), want)
	}
	for i := range under {
		if under[i].Size != int64(3+i) || !filepath.IsAbs(under[i].Abs) {
			t.Errorf("source %+v lacks its size or absolute path", under[i])
		}
	}
}

func TestWalkSingleFileLandsUnderTo(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "tokenizer.json")
	writeFile(t, file, patterned(9, 1))

	root, err := Walk([]string{file}, "")
	if err != nil || !slices.Equal(targets(root), []string{"tokenizer.json"}) {
		t.Errorf("at the root: %v, %v", targets(root), err)
	}
	under, err := Walk([]string{file}, "tokenizer")
	if err != nil || !slices.Equal(targets(under), []string{"tokenizer/tokenizer.json"}) {
		t.Errorf("under --to: %v, %v", targets(under), err)
	}
}

func TestWalkModeKeepsReadAndExecuteBits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no execute bit")
	}
	dir := t.TempDir()
	plain, script := filepath.Join(dir, "plain.bin"), filepath.Join(dir, "run.sh")
	writeFile(t, plain, patterned(3, 1))
	writeFile(t, script, patterned(3, 2))
	if err := os.Chmod(plain, 0o664); err != nil { //nolint:gosec // the group bits are under test
		t.Fatal(err)
	}
	if err := os.Chmod(script, 0o775); err != nil { //nolint:gosec // the execute bit is under test
		t.Fatal(err)
	}

	got, err := Walk([]string{dir}, "")
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Mode != 0o444 || got[1].Mode != 0o555 {
		t.Errorf("modes = %o and %o, want 444 and 555", got[0].Mode, got[1].Mode)
	}
}

func TestWalkSymlinks(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(base, "outside.bin")
	writeFile(t, outside, patterned(3, 1))
	root := filepath.Join(base, "weights")
	writeFile(t, filepath.Join(root, "real.bin"), patterned(4, 2))

	t.Run("inside the directory is followed", func(t *testing.T) {
		symlinkOrSkip(t, filepath.Join(root, "real.bin"), filepath.Join(root, "alias.bin"))
		t.Cleanup(func() { _ = os.Remove(filepath.Join(root, "alias.bin")) })

		got, err := Walk([]string{root}, "")
		if err != nil {
			t.Fatal(err)
		}
		if want := []string{"alias.bin", "real.bin"}; !slices.Equal(targets(got), want) {
			t.Errorf("targets = %v, want %v", targets(got), want)
		}
		if got[0].Abs != got[1].Abs || got[0].Size != 4 {
			t.Errorf("the link and its file should read the same file: %+v %+v", got[0], got[1])
		}
	})

	t.Run("out of the directory is refused", func(t *testing.T) {
		link := filepath.Join(root, "escape.bin")
		symlinkOrSkip(t, outside, link)
		t.Cleanup(func() { _ = os.Remove(link) })

		_, err := Walk([]string{root}, "")

		if !errors.Is(err, ErrEscapingSymlink) || !strings.Contains(err.Error(), "escape.bin") {
			t.Errorf("error = %v, want the escaping link named", err)
		}
	})

	t.Run("to a directory is refused", func(t *testing.T) {
		link := filepath.Join(root, "dirlink")
		symlinkOrSkip(t, root, link)
		t.Cleanup(func() { _ = os.Remove(link) })

		_, err := Walk([]string{root}, "")

		if !errors.Is(err, ErrSymlinkDirectory) && !errors.Is(err, ErrEscapingSymlink) {
			t.Errorf("error = %v, want the directory link refused", err)
		}
	})

	t.Run("named as a path of its own is followed", func(t *testing.T) {
		link := filepath.Join(base, "named.bin")
		symlinkOrSkip(t, outside, link)
		t.Cleanup(func() { _ = os.Remove(link) })

		got, err := Walk([]string{link}, "")
		if err != nil || !slices.Equal(targets(got), []string{"named.bin"}) || got[0].Size != 3 {
			t.Errorf("got %v, %v", got, err)
		}
	})
}

func TestWalkRefusesDuplicateTargets(t *testing.T) {
	base := t.TempDir()
	first, second := filepath.Join(base, "one"), filepath.Join(base, "two")
	writeFile(t, filepath.Join(first, "x.bin"), patterned(3, 1))
	writeFile(t, filepath.Join(second, "x.bin"), patterned(4, 2))

	for name, paths := range map[string][]string{
		"two directories with one file name": {first, second},
		"one directory twice":                {first, first},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Walk(paths, "")

			if !errors.Is(err, ErrDuplicateTarget) || !strings.Contains(err.Error(), "x.bin") {
				t.Errorf("error = %v, want the duplicate target x.bin", err)
			}
		})
	}
}

const reasonControl = "control"

type targetCase struct {
	name   string
	target string
	reason string
}

func TestCheckTarget(t *testing.T) {
	cases := []targetCase{
		{
			name:   "plain",
			target: "a/b.bin",
		},
		{
			name:   "dotfile",
			target: ".gitattributes",
		},
		{
			name:   "two dots and a name",
			target: "..hidden/x",
		},
		{
			name:   "unicode",
			target: "modèle/poids.bin",
		},
		{
			name:   "empty",
			target: "",
			reason: "empty",
		},
		{
			name:   "dot component",
			target: "a/./b",
			reason: `"."`,
		},
		{
			name:   "dot dot component",
			target: "a/../b",
			reason: `".."`,
		},
		{
			name:   "empty component",
			target: "a//b",
			reason: "empty component",
		},
		{
			name:   "leading slash",
			target: "/a",
			reason: "empty component",
		},
		{
			name:   "control character",
			target: "a/b\x01c",
			reason: reasonControl,
		},
		{
			name:   "DEL",
			target: "a\x7fb",
			reason: reasonControl,
		},
		{
			name:   "C1 control",
			target: "a\u0085b",
			reason: reasonControl,
		},
		{
			name:   "line separator",
			target: "a b",
			reason: reasonControl,
		},
		{
			name:   "not UTF-8",
			target: "a\xffb",
			reason: "UTF-8",
		},
		{
			name:   "too long",
			target: strings.Repeat("a", maxTargetBytes+1),
			reason: "over the",
		},
		{
			name:   "longest",
			target: strings.Repeat("a", maxTargetBytes),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := checkTarget(tc.target)
			if tc.reason == "" && got != "" {
				t.Errorf("checkTarget(%q) refused: %s", tc.target, got)
			}
			if tc.reason != "" && !strings.Contains(got, tc.reason) {
				t.Errorf("checkTarget(%q) = %q, want it to say %q", tc.target, got, tc.reason)
			}
		})
	}
}

func TestWalkRefusesInvalidDestination(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a.bin")
	writeFile(t, file, patterned(3, 1))

	for _, to := range []string{"../escape", "a//b", "a/./b", "ctrl\x01"} {
		if _, err := Walk([]string{file}, to); err == nil || !strings.Contains(err.Error(), "destination directory") {
			t.Errorf("--to %q: error = %v, want the destination refused", to, err)
		}
	}
}

func TestWalkMissingPath(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nope")

	_, err := Walk([]string{missing}, "")

	if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), "nope") {
		t.Errorf("error = %v, want the missing path named", err)
	}
}
