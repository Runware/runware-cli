package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// hookCase is a Bash command run through the hook in a directory holding
// files, and the exit code the hook must return.
// fileBody is the body file most cases post.
const (
	fileBody  = "body.md"
	cleanText = "clean text"
)

type hookCase struct {
	name    string
	command string
	files   map[string]string
	want    int
}

// runHookCases runs each case in its own copy of dir.
func runHookCases(t *testing.T, newDir func(t *testing.T) string, cases []hookCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := newDir(t)
			for name, text := range tc.files {
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var stderr bytes.Buffer
			payload := bytes.NewReader(bashPayload(t, tc.command, dir))
			if code := run(context.Background(), []string{modeHook}, payload, &bytes.Buffer{}, &stderr); code != tc.want {
				t.Fatalf("exit %d, want %d; stderr:\n%s", code, tc.want, stderr.String())
			}
		})
	}
}

// TestHookBlocksUnreadableForms covers commands that publish in a way the
// shell parser used to misread. Each must be blocked: by a finding when the
// hook can read the form, otherwise because it cannot.
func TestHookBlocksUnreadableForms(t *testing.T) {
	hiddenRepo := shortRepo + "some-private-repo"
	merge := `{"commit_title":"merge","commit_message":` + jsonString(leak) + `}`
	cases := []hookCase{
		{
			name:    "file substitution after cd",
			command: `cd sub && gh pr comment 1 --body "$(cat body.md)"`,
			files: map[string]string{
				fileBody:      cleanText,
				"sub/body.md": leak,
			},
		},
		{
			name:    "write inside an assignment's substitution",
			command: `url=$(gh pr create --title ok --body '` + leak + `')`,
		},
		{
			name:    "gh pr close comment",
			command: `gh pr close 1 --comment '` + leak + `'`,
		},
		{
			name:    "gh issue reopen comment",
			command: `gh issue reopen 1 -c '` + leak + `'`,
		},
		{
			name:    "gh pr merge subject",
			command: `gh pr merge 1 --squash --subject '` + leak + `'`,
		},
		{
			name:    "gh pr merge body",
			command: `gh pr merge 1 -b '` + leak + `'`,
		},
		{
			name:    "gh pr merge body file",
			command: `gh pr merge 1 --body-file body.md`,
			files: map[string]string{
				fileBody: leak,
			},
		},
		{
			name:    "repo flag between gh subcommands",
			command: `gh pr --repo Runware/runware-cli comment 1 --body '` + leak + `'`,
		},
		{
			name:    "repo flag before gh subcommands",
			command: `gh -R Runware/runware-cli pr comment 1 --body '` + leak + `'`,
		},
		{
			name:    "here-document written to the body file in the same command",
			command: "cat > body.md <<'EOF'\n" + leak + "\nEOF\ngh pr comment 1 --body-file body.md",
		},
		{
			name:    "stale body file rewritten in the same command",
			command: "cat > body.md <<'EOF'\n" + leak + "\nEOF\ngh pr comment 1 --body-file body.md",
			files: map[string]string{
				fileBody: cleanText,
			},
		},
		{
			name:    "REST merge commit message",
			command: `gh api -X PUT repos/Runware/runware-cli/pulls/1/merge -f commit_message='` + leak + `'`,
		},
		{
			name:    "REST merge commit title",
			command: `gh api -X PUT repos/Runware/runware-cli/pulls/1/merge -f commit_title='` + leak + `'`,
		},
		{
			name:    "REST merge with --input",
			command: `gh api -X PUT repos/Runware/runware-cli/pulls/1/merge --input merge.json`,
			files: map[string]string{
				"merge.json": merge,
			},
		},
		{
			name:    "printf with a newline format in a substitution",
			command: `gh pr comment 1 --body "$(printf '%s\n' 'see' '` + hiddenRepo + `')"`,
		},
		{
			name:    "pipe and redirection both feed stdin",
			command: `cat safe.md | gh pr comment 1 -F - < body.md`,
			files: map[string]string{
				"safe.md": cleanText,
				fileBody:  leak,
			},
		},
		{
			name:    "bash -c string",
			command: `bash -c "gh pr comment 1 --body 'x'"`,
		},
		{
			name:    "eval",
			command: `eval "gh pr comment 1 --body x"`,
		},
		{
			name:    "xargs",
			command: `echo 1 | xargs -I{} gh pr comment {} --body x`,
		},
		{
			name:    "backticks",
			command: "echo `gh pr comment 1 --body x`",
		},
		{
			name:    "subshell",
			command: `(gh pr comment 1 --body x)`,
		},
		{
			name:    "loop",
			command: `for n in 1 2; do gh pr comment "$n" --body x; done`,
		},
		{
			name:    "multi-line function",
			command: "post() {\n  gh pr comment 1 --body-file body.md\n}\ncp other.md body.md\npost",
			files: map[string]string{
				fileBody: cleanText,
			},
		},
		{
			name:    "variable body",
			command: `gh pr comment 1 --body "$BODY"`,
		},
		{
			name:    "variable command name",
			command: `$GH pr comment 1 --body x`,
		},
		{
			name:    "body file that does not exist",
			command: `gh pr comment 1 --body-file missing.md`,
		},
		{
			name:    "body file copied over first",
			command: `cp other.md body.md && gh pr comment 1 --body-file body.md`,
			files: map[string]string{
				fileBody: cleanText,
			},
		},
		{
			name:    "relative body file after cd",
			command: `cd sub && gh pr comment 1 --body-file body.md`,
			files: map[string]string{
				"sub/body.md": cleanText,
			},
		},
		{
			name:    "stdin from an unknown command",
			command: `make body | gh pr comment 1 -F -`,
		},
		{
			name:    "body from a variable the line sets",
			command: "body=$(cat <<'EOF'\n" + leak + "\nEOF\n); gh pr create --title x --body \"$body\"",
		},
		{
			name:    "echo of a variable set outside the line",
			command: `echo "$text" | gh pr comment 1 -F -`,
		},
		{
			name:    "echo of a variable the line sets",
			command: `text='` + leak + `'; echo "$text" | gh pr comment 1 -F -`,
		},
		{
			name:    "jq piped to gh api",
			command: `jq -n '{body: "` + leak + `"}' | gh api repos/Runware/runware-cli/issues/1/comments --input -`,
		},
		{
			name:    "reserved words",
			command: `if true; then gh pr comment 1 --body '` + leak + `'; fi`,
		},
		{
			name:    "bash -c with a body",
			command: `bash -c "gh pr comment 1 --body '` + leak + `'"`,
		},
		{
			name:    "timeout",
			command: `timeout 60 gh pr comment 1 --body '` + leak + `'`,
		},
		{
			name:    "body attached to its short flag",
			command: `gh pr comment 1 -b"` + leak + `"`,
		},
		{
			name:    "unquoted here-document that expands",
			command: "gh pr comment 1 -F - <<EOF\n$(cat body.md)\nEOF",
		},
	}
	for i := range cases {
		cases[i].want = exitBlock
	}
	runHookCases(t, func(t *testing.T) string {
		return t.TempDir()
	}, cases)
}

// TestHookURLDestination checks that a pull request URL, not the checkout,
// decides whether the destination is private.
func TestHookURLDestination(t *testing.T) {
	privateCheckout := func(t *testing.T) string {
		t.Helper()
		dir := t.TempDir()
		gitIn(t, dir, "init", "-q")
		gitIn(t, dir, "remote", "add", "origin", "https://"+ghRepo+"some-private-repo.git")
		return dir
	}
	runHookCases(t, privateCheckout, []hookCase{
		{
			name:    "public pull request URL from a private checkout",
			command: `gh pr comment https://github.com/Runware/runware-cli/pull/1 --body '` + leak + `'`,
			want:    exitBlock,
		},
		{
			name:    "comment in the private checkout",
			command: `gh pr comment 1 --body '` + leak + `'`,
			want:    0,
		},
	})
	runHookCases(t, func(t *testing.T) string {
		return t.TempDir()
	}, []hookCase{
		{
			name:    "private pull request URL",
			command: `gh pr comment https://` + ghRepo + `some-private-repo/pull/1 --body '` + leak + `'`,
			want:    0,
		},
	})
}

// TestHookAllowsSimpleForms checks that the forms people use to post and
// push still pass.
func TestHookAllowsSimpleForms(t *testing.T) {
	cases := []hookCase{
		{
			name:    "body file",
			command: `gh pr comment 1 --body-file existing.md`,
			files: map[string]string{
				"existing.md": cleanText,
			},
		},
		{
			name:    "body file then another command",
			command: `gh pr comment 1 --body-file existing.md && echo done`,
			files: map[string]string{
				"existing.md": cleanText,
			},
		},
		{
			name:    "literal title and body",
			command: `gh pr create --title "x" --body "y"`,
		},
		{
			name:    "here-document piped to gh",
			command: "cat <<'EOF' | gh pr comment 1 --body-file -\nclean text\nEOF",
		},
		{
			name:    "here-document on stdin",
			command: "gh pr comment 1 -F - <<'EOF'\nclean text\nEOF",
		},
		{
			name:    "printf %s piped to gh",
			command: `printf '%s' 'clean text' | gh pr comment 1 -F -`,
		},
		{
			name:    "here-document in a substitution",
			command: "gh pr create --title x --body \"$(cat <<'EOF'\nclean text\nEOF\n)\"",
		},
		{
			name:    "body from a variable the line sets",
			command: "body=$(cat <<'EOF'\nclean text\nEOF\n); gh pr create --title x --body \"$body\"",
		},
		{
			name:    "gh read",
			command: `gh pr view 1 --json body`,
		},
		{
			name:    "gh api read",
			command: `gh api repos/o/r/pulls/1/comments`,
		},
		{
			name:    "plain commands",
			command: `ls -la && echo hi | wc -l`,
		},
		{
			name:    "commit message from a substitution",
			command: "git commit -m \"$(cat <<'EOF'\nfix: mention gh pr comment and push\nEOF\n)\"",
		},
		{
			name:    "private destination",
			command: `gh pr comment 1 -R ` + shortRepo + `some-private-repo --body '` + leak + `'`,
		},
	}
	runHookCases(t, func(t *testing.T) string {
		return t.TempDir()
	}, cases)

	runHookCases(t, func(t *testing.T) string {
		return gitRepo(t, cleanText, "feat: add b")
	}, []hookCase{
		{
			name:    "git push",
			command: `git push`,
		},
		{
			name:    "git push origin branch",
			command: `git push origin feature`,
		},
		{
			name:    "git push after a read",
			command: `git status && git push -u origin HEAD`,
		},
	})
}

// TestHookPushForms checks git pushes the hook cannot follow.
func TestHookPushForms(t *testing.T) {
	runHookCases(t, func(t *testing.T) string {
		return gitRepo(t, cleanText, "feat: add b")
	}, []hookCase{
		{
			name:    "commit before push",
			command: `git commit -q --allow-empty -m '` + leak + `' && git push`,
			want:    exitBlock,
		},
		{
			name:    "variable refspec",
			command: `git push origin "$BRANCH"`,
			want:    exitBlock,
		},
		{
			name:    "git dir from the environment",
			command: `GIT_DIR=/elsewhere/.git git push`,
			want:    exitBlock,
		},
		{
			name:    "cd to a variable",
			command: `cd "$REPO" && git push`,
			want:    exitBlock,
		},
	})
}

// TestHookFollowTags checks that --follow-tags adds only the annotated tags
// on the pushed commits, as git does.
func TestHookFollowTags(t *testing.T) {
	unrelated := func(t *testing.T) string {
		t.Helper()
		dir := gitRepo(t, cleanText, "feat: add b")
		gitIn(t, dir, "checkout", "-q", "-b", "other", "origin/main")
		gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "other work")
		gitIn(t, dir, "tag", "-a", "v9.0.0", "-m", "release\n\n"+leak)
		gitIn(t, dir, "checkout", "-q", "feature")
		return dir
	}
	onBranch := func(t *testing.T) string {
		t.Helper()
		dir := gitRepo(t, cleanText, "feat: add b")
		gitIn(t, dir, "tag", "-a", "v1.1.0", "-m", "release\n\n"+leak)
		return dir
	}
	runHookCases(t, unrelated, []hookCase{
		{
			name:    "annotated tag on an unpushed branch",
			command: `git push --follow-tags`,
			want:    0,
		},
	})
	runHookCases(t, onBranch, []hookCase{
		{
			name:    "annotated tag on the pushed branch",
			command: `git push --follow-tags`,
			want:    exitBlock,
		},
	})
}
