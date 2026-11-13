package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	remoteOrigin = "origin"
	fileBody     = "body.md"
	cleanText    = "clean text"
	cmdGitPush   = "git push"
)

// leak is text with one finding, built at run time (see rules_test.go).
var leak = "see https://" + ghRepo + "some-private-repo/pull/1"

type shellCase struct {
	name string
	src  string
	want []shellCmd
}

func TestParseShell(t *testing.T) {
	cases := []shellCase{
		{
			name: "quotes and separators",
			src:  `cd /tmp && git commit -m "a \"b\" c" ; echo 'x  y'|wc -l`,
			want: []shellCmd{
				{args: []string{"cd", "/tmp"}},
				{args: []string{"git", "commit", "-m", `a "b" c`}},
				{args: []string{"echo", "x  y"}},
				{args: []string{"wc", "-l"}},
			},
		},
		{
			name: "redirections are not arguments",
			src:  `gh pr view 1 2>&1 >/dev/null`,
			want: []shellCmd{
				{args: []string{"gh", "pr", "view", "1"}},
			},
		},
		{
			name: "heredoc in command substitution keeps its source",
			src:  "gh pr create --title t --body \"$(cat <<'EOF'\nline (1)\n\"quoted\"\nEOF\n)\"",
			want: []shellCmd{
				{args: []string{"gh", "pr", "create", "--title", "t", "--body", "$(cat <<'EOF'\nline (1)\n\"quoted\"\nEOF\n)"}},
			},
		},
		{
			name: "heredoc on stdin",
			src:  "gh pr comment 1 -F - <<EOF\nhello\nEOF\necho done",
			want: []shellCmd{
				{
					args:     []string{"gh", "pr", "comment", "1", "-F", "-"},
					stdin:    "hello\n",
					hasStdin: true,
				},
				{args: []string{"echo", "done"}},
			},
		},
		{
			name: "here-string",
			src:  "gh pr comment 1 -F - <<< 'hi'",
			want: []shellCmd{
				{
					args:     []string{"gh", "pr", "comment", "1", "-F", "-"},
					stdin:    "hi\n",
					hasStdin: true,
				},
			},
		},
		{
			name: "comment and line continuation",
			src:  "git push \\\n  origin main # push it",
			want: []shellCmd{
				{args: []string{"git", "push", remoteOrigin, "main"}},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseShell(tc.src).cmds
			if len(got) != len(tc.want) {
				t.Fatalf("parseShell = %+v, want %+v", got, tc.want)
			}
			for i := range got {
				if strings.Join(got[i].args, "\x00") != strings.Join(tc.want[i].args, "\x00") || got[i].stdin != tc.want[i].stdin || got[i].hasStdin != tc.want[i].hasStdin {
					t.Errorf("command %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// hookCase is a Bash command run through the hook in a directory that setup
// makes (an empty one when nil) holding files, and whether the hook blocks
// it: by a finding, or because it cannot read the command completely.
type hookCase struct {
	name    string
	command string
	files   map[string]string
	setup   func(t *testing.T) string
	block   bool
}

// Directories the hook cases run in.
var (
	cleanRepo = func(t *testing.T) string {
		return gitRepo(t, cleanText, "feat: add b")
	}
	leakyCommit = func(t *testing.T) string {
		return gitRepo(t, cleanText, "feat: add b\n\n"+leak)
	}
	privateCheckout = func(t *testing.T) string {
		dir := t.TempDir()
		gitIn(t, dir, "init", "-q")
		gitIn(t, dir, "remote", "add", "origin", "https://"+ghRepo+"some-private-repo.git")
		return dir
	}
	// tagOnUnpushedBranch has an annotated tag with a leak on a commit the
	// push does not include; tagOnPushedBranch has one on the pushed commit.
	tagOnUnpushedBranch = func(t *testing.T) string {
		dir := cleanRepo(t)
		gitIn(t, dir, "checkout", "-q", "-b", "other", "origin/main")
		gitIn(t, dir, "commit", "-q", "--allow-empty", "-m", "other work")
		gitIn(t, dir, "tag", "-a", "v9.0.0", "-m", "release\n\n"+leak)
		gitIn(t, dir, "checkout", "-q", "feature")
		return dir
	}
	tagOnPushedBranch = func(t *testing.T) string {
		dir := cleanRepo(t)
		gitIn(t, dir, "tag", "-a", "v1.1.0", "-m", "release\n\n"+leak)
		return dir
	}
)

// TestHookForms runs the forms of posting and pushing through the hook. Every
// form that publishes a leak is blocked, by a finding when the hook reads it
// and otherwise because it cannot; the simple forms people use still pass.
func TestHookForms(t *testing.T) {
	hiddenRepo := shortRepo + "some-private-repo"
	review := `{"event":"COMMENT","body":"ok","comments":[{"path":"a.go","line":1,"body":` + jsonString(leak) + `}]}`
	mutation := `{"query":"mutation($input: AddCommentInput!){addComment(input:$input){clientMutationId}}","variables":{"input":{"subjectId":"x","note":` + jsonString(leak) + `}}}`
	readQuery := `{"query":"query{viewer{login}}","variables":{"q":` + jsonString(leak) + `}}`
	merge := `{"commit_title":"merge","commit_message":` + jsonString(leak) + `}`
	leak64 := base64.StdEncoding.EncodeToString([]byte("intro\n" + leak + "\n"))
	clean64 := base64.StdEncoding.EncodeToString([]byte(cleanText + "\n"))
	leakFile := map[string]string{
		fileBody: leak,
	}
	cleanFile := map[string]string{
		fileBody: cleanText,
	}
	cases := []hookCase{
		// Text in the command line itself.
		{
			name:    "literal title and body",
			command: `gh pr create --title "x" --body "y"`,
		},
		{
			name:    "leak in --body",
			command: `gh pr create --title "fix: x" --body "` + leak + `"`,
			block:   true,
		},
		{
			name:    "leak in -b with = forms",
			command: `gh pr create -t x -b "` + leak + `" --title=y`,
			block:   true,
		},
		{
			name:    "body attached to its short flag",
			command: `gh pr comment 1 -b"` + leak + `"`,
			block:   true,
		},
		{
			name:    "gh pr review body",
			command: `gh pr review 3 --comment -b "` + leak + `"`,
			block:   true,
		},
		{
			name:    "gh issue comment body",
			command: `gh issue comment 3 --body "` + leak + `"`,
			block:   true,
		},
		{
			name:    "gh pr close comment",
			command: `gh pr close 1 --comment '` + leak + `'`,
			block:   true,
		},
		{
			name:    "gh issue reopen comment",
			command: `gh issue reopen 1 -c '` + leak + `'`,
			block:   true,
		},
		{
			name:    "gh pr merge subject",
			command: `gh pr merge 1 --squash --subject '` + leak + `'`,
			block:   true,
		},
		{
			name:    "gh pr merge body",
			command: `gh pr merge 1 -b '` + leak + `'`,
			block:   true,
		},
		{
			name:    "repo flag between gh subcommands",
			command: `gh pr --repo Runware/runware-cli comment 1 --body '` + leak + `'`,
			block:   true,
		},
		{
			name:    "repo flag before gh subcommands",
			command: `gh -R Runware/runware-cli pr comment 1 --body '` + leak + `'`,
			block:   true,
		},
		{
			name:    "deny pattern",
			command: `gh pr comment 3 --body 'stored at fakevendor'`,
			block:   true,
		},
		{
			name:    "here-document in a substitution",
			command: "gh pr create --title x --body \"$(cat <<'EOF'\nclean text\nEOF\n)\"",
		},
		{
			name:    "leak in a here-document in a substitution",
			command: "gh pr create --title x --body \"$(cat <<'EOF'\n" + leak + "\nEOF\n)\"",
			block:   true,
		},
		{
			name:    "here-document on stdin",
			command: "gh pr comment 1 -F - <<'EOF'\nclean text\nEOF",
		},
		{
			name:    "leak in a here-document on stdin",
			command: "gh pr comment 3 -F - <<'EOF'\n" + leak + "\nEOF",
			block:   true,
		},
		{
			name:    "here-document piped to gh",
			command: "cat <<'EOF' | gh pr comment 1 --body-file -\nclean text\nEOF",
		},
		{
			name:    "here-string",
			command: "gh pr comment 3 -F - <<< 'clean text'",
		},
		{
			name:    "leak in a here-string",
			command: "gh pr comment 3 -F - <<< '" + leak + "'",
			block:   true,
		},
		{
			name:    "unquoted here-document that expands",
			command: "gh pr comment 1 -F - <<EOF\n$(cat body.md)\nEOF",
			files:   cleanFile,
			block:   true,
		},

		// Files the command posts.
		{
			name:    "body file",
			command: `gh pr comment 1 --body-file body.md`,
			files:   cleanFile,
		},
		{
			name:    "body file then another command",
			command: `gh pr comment 1 --body-file body.md && echo done`,
			files:   cleanFile,
		},
		{
			name:    "leak in a body file",
			command: `gh pr create --title x --body-file body.md`,
			files:   leakFile,
			block:   true,
		},
		{
			name:    "leak in a -F body file",
			command: `gh pr edit 3 -F body.md`,
			files:   leakFile,
			block:   true,
		},
		{
			name:    "gh pr merge body file",
			command: `gh pr merge 1 --body-file body.md`,
			files:   leakFile,
			block:   true,
		},
		{
			name:    "stdin from a file",
			command: `gh pr comment 3 -F - < body.md`,
			files:   cleanFile,
		},
		{
			name:    "leak in stdin from a file",
			command: `gh pr comment 3 -F - < body.md`,
			files:   leakFile,
			block:   true,
		},
		{
			name:    "body file that does not exist",
			command: `gh pr comment 1 --body-file missing.md`,
			block:   true,
		},
		{
			name:    "here-document written to the body file",
			command: "cat > body.md <<'EOF'\n" + leak + "\nEOF\ngh pr comment 1 --body-file body.md",
			block:   true,
		},
		{
			name:    "stale body file rewritten",
			command: "cat > body.md <<'EOF'\n" + leak + "\nEOF\ngh pr comment 1 --body-file body.md",
			files:   cleanFile,
			block:   true,
		},
		{
			name:    "body file copied over first",
			command: `cp other.md body.md && gh pr comment 1 --body-file body.md`,
			files:   cleanFile,
			block:   true,
		},
		{
			name:    "relative body file after cd",
			command: `cd sub && gh pr comment 1 --body-file body.md`,
			files:   map[string]string{"sub/body.md": cleanText},
			block:   true,
		},
		{
			name:    "file substitution after cd",
			command: `cd sub && gh pr comment 1 --body "$(cat body.md)"`,
			files: map[string]string{
				fileBody:      cleanText,
				"sub/body.md": leak,
			},
			block: true,
		},
		{
			name:    "cat of a file in a substitution",
			command: `gh pr comment 3 --body "$(cat body.md)"`,
			files:   cleanFile,
			block:   true,
		},
		{
			name:    "printf in a substitution",
			command: `gh pr comment 1 --body "$(printf '%s\n' 'see' '` + hiddenRepo + `')"`,
			block:   true,
		},
		{
			name:    "printf piped to gh",
			command: `printf '%s' 'clean text' | gh pr comment 1 -F -`,
			block:   true,
		},
		{
			name:    "pipe and redirection both feed stdin",
			command: `cat safe.md | gh pr comment 1 -F - < body.md`,
			files: map[string]string{
				"safe.md": cleanText,
				fileBody:  leak,
			},
			block: true,
		},
		{
			name:    "stdin from an unknown command",
			command: `make body | gh pr comment 1 -F -`,
			block:   true,
		},
		{
			name:    "echo of a variable piped to gh",
			command: `echo "$text" | gh pr comment 1 -F -`,
			block:   true,
		},
		{
			name:    "echo of a variable the line sets",
			command: `text='` + leak + `'; echo "$text" | gh pr comment 1 -F -`,
			block:   true,
		},
		{
			name:    "jq piped to gh api",
			command: `jq -n '{body: "` + leak + `"}' | gh api repos/Runware/runware-cli/issues/1/comments --input -`,
			block:   true,
		},

		// Variables, wrappers and compound commands.
		{
			name:    "variable body",
			command: `gh pr comment 1 --body "$BODY"`,
			block:   true,
		},
		{
			name:    "body from a variable the line sets",
			command: "body=$(cat <<'EOF'\nclean text\nEOF\n); gh pr create --title x --body \"$body\"",
			block:   true,
		},
		{
			name:    "variable command name",
			command: `$GH pr comment 1 --body x`,
			block:   true,
		},
		{
			name:    "write inside an assignment's substitution",
			command: `url=$(gh pr create --title ok --body '` + leak + `')`,
			block:   true,
		},
		{
			name:    "bash -c string",
			command: `bash -c "gh pr comment 1 --body 'x'"`,
			block:   true,
		},
		{
			name:    "eval",
			command: `eval "gh pr comment 1 --body x"`,
			block:   true,
		},
		{
			name:    "xargs",
			command: `echo 1 | xargs -I{} gh pr comment {} --body x`,
			block:   true,
		},
		{
			name:    "timeout",
			command: `timeout 60 gh pr comment 1 --body '` + leak + `'`,
			block:   true,
		},
		{
			name:    "backticks",
			command: "echo `gh pr comment 1 --body x`",
			block:   true,
		},
		{
			name:    "subshell",
			command: `(gh pr comment 1 --body x)`,
			block:   true,
		},
		{
			name:    "loop",
			command: `for n in 1 2; do gh pr comment "$n" --body x; done`,
			block:   true,
		},
		{
			name:    "reserved words",
			command: `if true; then gh pr comment 1 --body '` + leak + `'; fi`,
			block:   true,
		},
		{
			name:    "multi-line function",
			command: "post() {\n  gh pr comment 1 --body-file body.md\n}\ncp other.md body.md\npost",
			files:   cleanFile,
			block:   true,
		},

		// Releases.
		{
			name:    "release with clean notes",
			command: `gh release create v1.2.0 -t "v1.2.0" -n "clean text"`,
		},
		{
			name:    "release title",
			command: `gh release create v1.2.0 -t '` + leak + `'`,
			block:   true,
		},
		{
			name:    "release notes",
			command: `gh release edit v1.2.0 --notes '` + leak + `'`,
			block:   true,
		},
		{
			name:    "release notes file",
			command: `gh release create v1.2.0 -F body.md`,
			files:   leakFile,
			block:   true,
		},
		{
			name:    "release notes file that does not exist",
			command: `gh release create v1.2.0 --notes-file missing.md`,
			block:   true,
		},
		{
			name:    "release on a private repository",
			command: `gh release create v1.2.0 -R ` + hiddenRepo + ` -n '` + leak + `'`,
		},
		{
			name:    "release from a private checkout",
			command: `gh release create v1.2.0 -n '` + leak + `'`,
			setup:   privateCheckout,
		},

		// gh api.
		{
			name:    "gh api read",
			command: `gh api repos/Runware/runware-cli/pulls/3 --jq .body`,
		},
		{
			name:    "gh api comment with -f",
			command: `gh api repos/Runware/runware-cli/issues/3/comments -f body="` + leak + `"`,
			block:   true,
		},
		{
			name:    "gh api -F body=@file",
			command: `gh api -X POST repos/{owner}/{repo}/pulls/3/comments -F body=@body.md -f path=x.go`,
			files:   leakFile,
			block:   true,
		},
		{
			name:    "gh api review with nested -f fields",
			command: `gh api repos/Runware/runware-cli/pulls/3/reviews -f event=COMMENT -f 'comments[][path]=a.go' -f 'comments[][body]=` + leak + `'`,
			block:   true,
		},
		{
			name:    "gh api graphql mutation",
			command: `gh api graphql -f query='mutation($b:String!){addComment(input:{subjectId:"x",body:$b}){clientMutationId}}' -f b="` + leak + `"`,
			block:   true,
		},
		{
			name:    "gh api review in --input",
			command: `gh api repos/Runware/runware-cli/pulls/3/reviews --input review.json`,
			files:   map[string]string{"review.json": review},
			block:   true,
		},
		{
			name:    "gh api graphql mutation in --input",
			command: `gh api graphql --input mutation.json`,
			files:   map[string]string{"mutation.json": mutation},
			block:   true,
		},
		{
			name:    "gh api graphql query in --input",
			command: `gh api graphql --input query.json`,
			files:   map[string]string{"query.json": readQuery},
			block:   true,
		},
		{
			name:    "REST merge commit message",
			command: `gh api -X PUT repos/Runware/runware-cli/pulls/1/merge -f commit_message='` + leak + `'`,
			block:   true,
		},
		{
			name:    "REST merge commit title",
			command: `gh api -X PUT repos/Runware/runware-cli/pulls/1/merge -f commit_title='` + leak + `'`,
			block:   true,
		},
		{
			name:    "REST merge with --input",
			command: `gh api -X PUT repos/Runware/runware-cli/pulls/1/merge --input merge.json`,
			files:   map[string]string{"merge.json": merge},
			block:   true,
		},

		// Contents API writes send the file base64-encoded.
		{
			name:    "Contents API file in --input",
			command: `gh api -X PUT repos/Runware/runware-cli/contents/a.md --input put.json`,
			files: map[string]string{
				"put.json": `{"message":"docs: a","content":"` + leak64 + `"}`,
			},
			block: true,
		},
		{
			name:    "Contents API file in -f content",
			command: `gh api -X PUT repos/Runware/runware-cli/contents/a.md -f message=docs -f content=` + leak64,
			block:   true,
		},
		{
			name:    "Contents API file in -F content=@file",
			command: `gh api -X PUT repos/Runware/runware-cli/contents/a.md -f message=docs -F content=@a.b64`,
			files: map[string]string{
				"a.b64": leak64,
			},
			block: true,
		},
		{
			name:    "Contents API clean file",
			command: `gh api -X PUT repos/Runware/runware-cli/contents/a.md -f message=docs -f content=` + clean64,
		},

		// gh pr create --fill posts commit messages the hook does not read.
		{
			name:    "pr create --fill",
			command: `gh pr create --fill`,
			block:   true,
		},
		{
			name:    "pr create --fill-first",
			command: `gh pr create --fill-first --draft`,
			block:   true,
		},
		{
			name:    "pr create --fill-verbose with a title",
			command: `gh pr create --fill-verbose --title x`,
			block:   true,
		},
		{
			name:    "pr create -f grouped with -d",
			command: `gh pr create -df`,
			block:   true,
		},
		{
			name:    "pr create --fill with a title and body",
			command: `gh pr create --fill --title x --body-file body.md`,
			files:   cleanFile,
		},
		{
			name:    "pr create -f as a value of -t",
			command: `gh pr create -t -f -b y`,
		},

		// Expansions the shell runs.
		{
			name:    "variable read from a file",
			command: `body=clean; read -r body < body.md; gh pr comment 1 --body "$body"`,
			files:   leakFile,
			block:   true,
		},
		{
			name:    "variable appended to",
			command: `body=clean; body+=" more"; gh pr comment 1 --body "$body"`,
			block:   true,
		},
		{
			name:    "write in a parameter expansion default",
			command: `result=${result:-$(gh pr comment 1 --body-file body.md)}`,
			files:   leakFile,
			block:   true,
		},
		{
			name:    "write in backticks in a quoted parameter expansion",
			command: "echo \"${x:-`gh pr comment 1 --body-file body.md`}\"",
			files:   leakFile,
			block:   true,
		},
		{
			name:    "plain parameter expansion",
			command: `echo "${HOME:-/tmp}/x"`,
		},

		// Destinations.
		{
			name:    "gh read",
			command: `gh pr view 1 --json body`,
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
			command: `gh pr comment 1 -R ` + hiddenRepo + ` --body '` + leak + `'`,
		},
		{
			name:    "gh api on a private repository",
			command: `gh api repos/` + hiddenRepo + `/issues/3/comments -f body="` + leak + `"`,
		},
		{
			name:    "private pull request URL",
			command: `gh pr comment https://` + ghRepo + `some-private-repo/pull/1 --body '` + leak + `'`,
		},
		{
			name:    "comment in a private checkout",
			command: `gh pr comment 1 --body '` + leak + `'`,
			setup:   privateCheckout,
		},
		{
			name:    "public pull request URL from a private checkout",
			command: `gh pr comment https://github.com/Runware/runware-cli/pull/1 --body '` + leak + `'`,
			setup:   privateCheckout,
			block:   true,
		},
		{
			name:    "gh api {owner}/{repo} from a private checkout",
			command: `gh api repos/{owner}/{repo}/issues/1/comments -f body='` + leak + `'`,
			setup:   privateCheckout,
		},
		{
			name:    "gh api {owner} with a public name from a private checkout",
			command: `gh api repos/{owner}/runware-cli/issues/1/comments -f body='` + leak + `'`,
			setup:   privateCheckout,
			block:   true,
		},
		{
			name:    "gh api {owner} with a public name from an unknown checkout",
			command: `gh api repos/{owner}/runware-cli/issues/1/comments -f body='` + leak + `'`,
			block:   true,
		},
		{
			name:    "graphql from a private checkout",
			command: `gh api graphql -f query='mutation{x}' -f b='` + leak + `'`,
			setup:   privateCheckout,
			block:   true,
		},

		// git push.
		{
			name:    "git push",
			command: cmdGitPush,
			setup:   cleanRepo,
		},
		{
			name:    "git push origin branch",
			command: `git push origin feature`,
			setup:   cleanRepo,
		},
		{
			name:    "git push after a read",
			command: `git status && git push -u origin HEAD`,
			setup:   cleanRepo,
		},
		{
			name:    "leak in a pushed commit",
			command: `git -C . push -u origin HEAD`,
			setup:   leakyCommit,
			block:   true,
		},
		{
			name:    "push behind env and wrapper",
			command: `GIT_TRACE=0 command git push --force-with-lease`,
			setup:   leakyCommit,
			block:   true,
		},
		{
			name:    "not a push",
			command: `git log --oneline | grep push`,
			setup:   leakyCommit,
		},
		{
			name:    "commit before push",
			command: `git commit -q --allow-empty -m '` + leak + `' && git push`,
			setup:   cleanRepo,
			block:   true,
		},
		{
			name:    "variable refspec",
			command: `git push origin "$BRANCH"`,
			setup:   cleanRepo,
			block:   true,
		},
		{
			name:    "git dir from the environment",
			command: `GIT_DIR=/elsewhere/.git git push`,
			setup:   cleanRepo,
			block:   true,
		},
		{
			name:    "cd to a variable",
			command: `cd "$REPO" && git push`,
			setup:   cleanRepo,
			block:   true,
		},
		{
			name:    "fetch URL private, push URL public",
			command: cmdGitPush,
			setup:   pushURLs("some-private-repo", repoCLI),
			block:   true,
		},
		{
			name:    "fetch and push URLs private",
			command: cmdGitPush,
			setup:   pushURLs("some-private-repo", "some-private-repo"),
		},
		{
			name:    "annotated tag on an unpushed branch",
			command: `git push --follow-tags`,
			setup:   tagOnUnpushedBranch,
		},
		{
			name:    "annotated tag on the pushed branch",
			command: `git push --follow-tags`,
			setup:   tagOnPushedBranch,
			block:   true,
		},
	}
	s, err := NewScanner(nil)
	if err != nil {
		t.Fatal(err)
	}
	s.loadDeny = func() []string {
		return []string{`\bfakevendor\b`}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.setup != nil {
				dir = tc.setup(t)
			}
			for name, text := range tc.files {
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			f, err := s.runHook(context.Background(), bashPayload(t, tc.command, dir), gitRepoOf)
			if err != nil && !errors.As(err, new(*UncheckableError)) {
				t.Fatal(err)
			}
			if blocked := err != nil || len(f) > 0; blocked != tc.block {
				t.Fatalf("blocked = %v (findings %+v, error %v), want %v", blocked, f, err, tc.block)
			}
		})
	}
}

// pushURLs returns a setup that makes leakyCommit's repository with an origin
// that fetches from the Runware repository fetch and pushes to push.
func pushURLs(fetch, push string) func(t *testing.T) string {
	return func(t *testing.T) string {
		dir := leakyCommit(t)
		gitIn(t, dir, "remote", "add", "origin", "https://"+ghRepo+fetch+".git")
		gitIn(t, dir, "remote", "set-url", "--push", "origin", "https://"+ghRepo+push+".git")
		return dir
	}
}

// gitRepo makes a repository with origin/main and a branch on top of it whose
// commit adds text and has message as its message.
func gitRepo(t *testing.T, text, message string) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...) //nolint:gosec // test arguments
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "test")
	git("config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-q", "-m", "base")
	git("update-ref", "refs/remotes/origin/main", "HEAD")
	git("checkout", "-q", "-b", "feature")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte(text+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-q", "-m", message)
	return dir
}

func TestHookBlocksPush(t *testing.T) {
	dir := gitRepo(t, cleanText, "feat: add b\n\n"+leak)
	s, err := NewScanner(nil)
	if err != nil {
		t.Fatal(err)
	}
	payload := bashPayload(t, "git push -u origin feature", dir)
	f, err := s.runHook(context.Background(), payload, gitRepoOf)
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 1 || !strings.HasPrefix(f[0].Where, "commit ") {
		t.Fatalf("findings = %+v, want one in the commit message", f)
	}
}

func TestHookModeExitCode(t *testing.T) {
	payload := `{"tool_name":"Bash","tool_input":{"command":` + jsonString(`gh pr comment 1 -R Runware/runware-cli --body "`+leak+`"`) + `}}`
	var stderr bytes.Buffer
	if code := run(context.Background(), []string{modeHook}, strings.NewReader(payload), &bytes.Buffer{}, &stderr); code != exitBlock {
		t.Fatalf("exit %d, want %d: %s", code, exitBlock, stderr.String())
	}
	if !strings.Contains(stderr.String(), "some-private-repo") {
		t.Errorf("stderr does not explain the finding: %s", stderr.String())
	}
	other := `{"tool_name":"Edit","tool_input":{}}`
	if code := run(context.Background(), []string{modeHook}, strings.NewReader(other), &bytes.Buffer{}, &stderr); code != 0 {
		t.Fatalf("exit %d for another tool", code)
	}
}

func TestHookModeErrorBlocks(t *testing.T) {
	var stderr bytes.Buffer
	if code := run(context.Background(), []string{modeHook}, strings.NewReader("not json"), &bytes.Buffer{}, &stderr); code != exitBlock {
		t.Fatalf("exit %d for a payload the hook cannot read, want %d: %s", code, exitBlock, stderr.String())
	}
	if code := run(context.Background(), []string{modeHook, "-unknown"}, strings.NewReader("{}"), &bytes.Buffer{}, &stderr); code != exitBlock {
		t.Fatalf("exit %d for a bad flag, want %d", code, exitBlock)
	}
}

func TestPrePush(t *testing.T) {
	dir := gitRepo(t, leak, "feat: add b")
	g := Git{
		Dir: dir,
	}
	ctx := context.Background()
	head, err := g.revParse(ctx, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	zero := strings.Repeat("0", 40)
	s, err := NewScanner(nil)
	if err != nil {
		t.Fatal(err)
	}

	newBranch := "refs/heads/feature " + head + " refs/heads/feature " + zero + "\n"
	f, err := s.prePush(ctx, g, []string{remoteOrigin}, strings.NewReader(newBranch))
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 1 || f[0].Where != "b.txt:1" {
		t.Fatalf("findings = %+v, want one at b.txt:1", f)
	}

	privateURL := "git@github.com:" + shortRepo + "some-private-repo.git"
	if f, err := s.prePush(ctx, g, []string{"other", privateURL}, strings.NewReader(newBranch)); err != nil || len(f) != 0 {
		t.Fatalf("findings for a push to a private repository = %+v, %v", f, err)
	}

	upToDate := "refs/heads/feature " + head + " refs/heads/feature " + head + "\n"
	if f, err := s.prePush(ctx, g, []string{remoteOrigin}, strings.NewReader(upToDate)); err != nil || len(f) != 0 {
		t.Fatalf("findings for a push with nothing new = %+v, %v", f, err)
	}

	deletion := "(delete) " + zero + " refs/heads/feature " + head + "\n"
	if f, err := s.prePush(ctx, g, []string{remoteOrigin}, strings.NewReader(deletion)); err != nil || len(f) != 0 {
		t.Fatalf("findings for a deletion = %+v, %v", f, err)
	}
}

// TestScanRangeHistory checks that a leak added by one commit and removed by
// the next is still found: the history is published, not only the tree.
func TestScanRangeHistory(t *testing.T) {
	dir := gitRepo(t, leak, "feat: add b")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("clean text\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "commit", "-q", "-am", "fix: remove it")
	s, err := NewScanner(nil)
	if err != nil {
		t.Fatal(err)
	}
	f, err := s.ScanRange(context.Background(), Git{Dir: dir}, Range{
		Base: "origin/main",
		Head: "HEAD",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 1 || !strings.HasPrefix(f[0].Where, "commit ") {
		t.Fatalf("findings = %+v, want one in a commit", f)
	}
}

// TestPrePushAfterRebase checks a force-push of a branch rebased onto a newer
// main: the main commits in between are on the remote already and are not
// the pusher's to fix, so a leak in one of them does not block the push.
func TestPrePushAfterRebase(t *testing.T) {
	dir := gitRepo(t, cleanText, "feat: add b")
	old := gitIn(t, dir, "rev-parse", "HEAD")
	gitIn(t, dir, "update-ref", "refs/remotes/origin/feature", old)
	gitIn(t, dir, "checkout", "-q", "-b", "newmain", "origin/main")
	if err := os.WriteFile(filepath.Join(dir, "m.txt"), []byte(leak+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-q", "-m", "feat: someone else's work")
	gitIn(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
	gitIn(t, dir, "checkout", "-q", "feature")
	gitIn(t, dir, "rebase", "-q", "newmain")
	head := gitIn(t, dir, "rev-parse", "HEAD")
	s, err := NewScanner(nil)
	if err != nil {
		t.Fatal(err)
	}
	line := "refs/heads/feature " + head + " refs/heads/feature " + old + "\n"
	f, err := s.prePush(context.Background(), Git{Dir: dir}, []string{remoteOrigin}, strings.NewReader(line))
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 0 {
		t.Fatalf("findings = %+v, want none from main's commits", f)
	}
}

// TestAddedPathIsScanned checks that the path of a file added or renamed in
// the range is scanned once, however many commits touch it.
func TestAddedPathIsScanned(t *testing.T) {
	name := "notes-" + clusterHost + ".md"
	added := gitRepo(t, cleanText, "feat: add b")
	if err := os.WriteFile(filepath.Join(added, name), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, added, "add", ".")
	gitIn(t, added, "commit", "-q", "-m", "docs: add notes")
	if err := os.WriteFile(filepath.Join(added, name), []byte("two\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	gitIn(t, added, "commit", "-q", "-am", "docs: edit notes")
	renamed := gitRepo(t, cleanText, "feat: add b")
	gitIn(t, renamed, "mv", "a.txt", name)
	gitIn(t, renamed, "commit", "-q", "-m", "chore: rename a")
	s, err := NewScanner(nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{added, renamed} {
		f, err := s.ScanRange(context.Background(), Git{Dir: dir}, Range{
			Base: "origin/main",
			Head: "HEAD",
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(f) != 1 || f[0].Where != "path of an added file" {
			t.Fatalf("findings = %+v, want one in the path", f)
		}
	}
}

func TestBranchNameIsScanned(t *testing.T) {
	dir := gitRepo(t, "clean", "feat: add b")
	s, err := NewScanner(nil)
	if err != nil {
		t.Fatal(err)
	}
	g := Git{
		Dir: dir,
	}
	f, err := s.ScanRange(context.Background(), g, Range{
		Base:   "origin/main",
		Head:   "HEAD",
		Branch: "fix/" + clusterHost,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 1 || f[0].Where != "branch name" {
		t.Fatalf("findings = %+v", f)
	}
}

func jsonString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}

// gitIn runs git in dir with the user's configuration ignored.
func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), "git", append([]string{"-C", dir}, args...)...) //nolint:gosec // test arguments
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// tagRepo is gitRepo with an annotated tag v1.0.0, whose message holds a
// leak, and a lightweight tag v0.9.0, both on the commit origin/main has
// already, and a lightweight tag v1.1.0 on the new commit.
func tagRepo(t *testing.T, text string) string {
	t.Helper()
	dir := gitRepo(t, text, "feat: add b")
	gitIn(t, dir, "tag", "-a", "v1.0.0", "-m", "release\n\n"+leak, "origin/main")
	gitIn(t, dir, "tag", "v0.9.0", "origin/main")
	gitIn(t, dir, "tag", "v1.1.0", "HEAD")
	return dir
}

func TestPrePushTag(t *testing.T) {
	dir := tagRepo(t, leak)
	g := Git{
		Dir: dir,
	}
	ctx := context.Background()
	s, err := NewScanner(nil)
	if err != nil {
		t.Fatal(err)
	}
	zero := strings.Repeat("0", 40)
	line := func(tag string) string {
		return "refs/tags/" + tag + " " + gitIn(t, dir, "rev-parse", "refs/tags/"+tag) + " refs/tags/" + tag + " " + zero + "\n"
	}

	f, err := s.prePush(ctx, g, []string{remoteOrigin}, strings.NewReader(line("v1.0.0")))
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 1 || f[0].Where != "tag v1.0.0 message line 3" {
		t.Fatalf("findings for an annotated tag = %+v, want one in its message", f)
	}

	if f, err := s.prePush(ctx, g, []string{remoteOrigin}, strings.NewReader(line("v0.9.0"))); err != nil || len(f) != 0 {
		t.Fatalf("findings for a tag on a published commit = %+v, %v", f, err)
	}

	f, err = s.prePush(ctx, g, []string{remoteOrigin}, strings.NewReader(line("v1.1.0")))
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 1 || f[0].Where != "b.txt:1" {
		t.Fatalf("findings for a tag on a new commit = %+v, want one at b.txt:1", f)
	}

	if f := s.ScanText("tag name", "v1-"+clusterHost); len(f) != 1 {
		t.Fatalf("tag name findings = %+v", f)
	}
}

type tagPushCase struct {
	command string
	want    int
}

func TestHookTagPush(t *testing.T) {
	dir := tagRepo(t, cleanText)
	s, err := NewScanner(nil)
	if err != nil {
		t.Fatal(err)
	}
	cases := []tagPushCase{
		{
			command: "git push origin v1.0.0",
			want:    1,
		},
		{
			command: "git push origin tag v1.0.0",
			want:    1,
		},
		{
			command: "git push origin refs/tags/v1.0.0",
			want:    1,
		},
		{
			command: "git push origin v1.0.0:refs/tags/v1.0.0",
			want:    1,
		},
		{
			command: "git push --tags",
			want:    1,
		},
		{
			command: "git push origin v0.9.0",
			want:    0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			payload := bashPayload(t, tc.command, dir)
			f, err := s.runHook(context.Background(), payload, gitRepoOf)
			if err != nil {
				t.Fatal(err)
			}
			if len(f) != tc.want {
				t.Fatalf("findings = %+v, want %d", f, tc.want)
			}
			if tc.want == 1 && f[0].Where != "tag v1.0.0 message line 3" {
				t.Errorf("finding at %q, want the tag message", f[0].Where)
			}
		})
	}
}
