package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const (
	labelCreateTitle = "gh pr create title"
	remoteOrigin     = "origin"
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
			name: "heredoc in command substitution",
			src:  "gh pr create --title t --body \"$(cat <<'EOF'\nline (1)\n\"quoted\"\nEOF\n)\"",
			want: []shellCmd{
				{args: []string{"gh", "pr", "create", "--title", "t", "--body", "line (1)\n\"quoted\""}},
			},
		},
		{
			name: "heredoc on stdin",
			src:  "gh pr comment 1 -F - <<EOF\nhello\nEOF\necho done",
			want: []shellCmd{
				{args: []string{"gh", "pr", "comment", "1", "-F", "-"}, stdin: "hello\n", hasStdin: true},
				{args: []string{"echo", "done"}},
			},
		},
		{
			name: "here-string",
			src:  "gh pr comment 1 -F - <<< 'hi'",
			want: []shellCmd{
				{args: []string{"gh", "pr", "comment", "1", "-F", "-"}, stdin: "hi\n", hasStdin: true},
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
			got := parseShell(tc.src, "")
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

func TestParseShellPipedHeredoc(t *testing.T) {
	cmds := parseShell("cat <<'EOF' | gh pr comment 1 --body-file -\nhello\nEOF\n", "")
	if len(cmds) != 2 || cmds[1].stdin != "hello\n" {
		t.Fatalf("parseShell = %+v, want the heredoc on gh's stdin", cmds)
	}
}

type planCase struct {
	name    string
	command string
	labels  []string
	pushes  int
}

func TestPlanCommand(t *testing.T) {
	dir := t.TempDir()
	bodyFile := filepath.Join(dir, "body.md")
	if err := os.WriteFile(bodyFile, []byte(leak), 0o600); err != nil {
		t.Fatal(err)
	}
	review := `{"event":"COMMENT","body":"ok","comments":[{"path":"a.go","line":1,"body":` + jsonString(leak) + `}]}`
	if err := os.WriteFile(filepath.Join(dir, "review.json"), []byte(review), 0o600); err != nil {
		t.Fatal(err)
	}
	mutation := `{"query":"mutation($input: AddCommentInput!){addComment(input:$input){clientMutationId}}","variables":{"input":{"subjectId":"x","note":` + jsonString(leak) + `}}}`
	if err := os.WriteFile(filepath.Join(dir, "mutation.json"), []byte(mutation), 0o600); err != nil {
		t.Fatal(err)
	}
	readQuery := `{"query":"query{viewer{login}}","variables":{"q":` + jsonString(leak) + `}}`
	if err := os.WriteFile(filepath.Join(dir, "query.json"), []byte(readQuery), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []planCase{
		{
			name:    "gh pr create with --body and --title",
			command: `gh pr create --title "fix: x" --body "` + leak + `"`,
			labels:  []string{labelCreateTitle, "gh pr create body"},
		},
		{
			name:    "gh pr create with -t and -b and = forms",
			command: `gh pr create -t x -b "` + leak + `" --title=y`,
			labels:  []string{labelCreateTitle, "gh pr create body", labelCreateTitle},
		},
		{
			name:    "gh pr create with --body-file",
			command: "gh pr create --title x --body-file body.md",
			labels:  []string{labelCreateTitle, "gh pr create body file"},
		},
		{
			name:    "gh pr edit with -F",
			command: "gh pr edit 3 -F " + bodyFile,
			labels:  []string{"gh pr edit body file"},
		},
		{
			name:    "gh pr review",
			command: `gh pr review 3 --comment -b "` + leak + `"`,
			labels:  []string{"gh pr review body"},
		},
		{
			name:    "gh issue comment",
			command: `gh issue comment 3 --body "` + leak + `"`,
			labels:  []string{"gh issue comment body"},
		},
		{
			name:    "gh on a private repository",
			command: `gh pr comment 3 -R ` + shortRepo + `some-private-repo --body "` + leak + `"`,
			labels:  nil,
		},
		{
			name:    "gh read",
			command: "gh pr view 3 --json body",
			labels:  nil,
		},
		{
			name:    "gh api comment with -f",
			command: `gh api repos/Runware/runware-cli/issues/3/comments -f body="` + leak + `"`,
			labels:  []string{"gh api field body"},
		},
		{
			name:    "gh api comment with -F body=@file",
			command: "gh api -X POST repos/{owner}/{repo}/pulls/3/comments -F body=@body.md -f path=x.go",
			labels:  []string{"gh api field body"},
		},
		{
			name:    "gh api on a private repository",
			command: `gh api repos/` + shortRepo + `some-private-repo/issues/3/comments -f body="` + leak + `"`,
			labels:  nil,
		},
		{
			name:    "gh api read",
			command: "gh api repos/Runware/runware-cli/pulls/3 --jq .body",
			labels:  nil,
		},
		{
			name:    "gh api graphql mutation",
			command: `gh api graphql -f query='mutation($b:String!){addComment(input:{subjectId:"x",body:$b}){clientMutationId}}' -f b="` + leak + `"`,
			labels:  []string{"gh api field query", "gh api field b"},
		},
		{
			name:    "gh api review with nested comments in --input",
			command: "gh api repos/Runware/runware-cli/pulls/3/reviews --input review.json",
			labels:  []string{"gh api --input body", "gh api --input comments[0].body"},
		},
		{
			name:    "gh api graphql mutation with --input",
			command: "gh api graphql --input mutation.json",
			labels:  []string{"gh api --input query", "gh api --input variables.input.note", "gh api --input variables.input.subjectId"},
		},
		{
			name:    "gh api graphql read with --input",
			command: "gh api graphql --input query.json",
			labels:  nil,
		},
		{
			name:    "gh api review with nested -f fields",
			command: `gh api repos/Runware/runware-cli/pulls/3/reviews -f event=COMMENT -f 'comments[][path]=a.go' -f 'comments[][body]=` + leak + `'`,
			labels:  []string{"gh api field comments[][body]"},
		},
		{
			name:    "git push",
			command: "cd sub && git -C . push -u origin HEAD",
			pushes:  1,
		},
		{
			name:    "git push behind env and wrapper",
			command: "GIT_TRACE=0 command git push --force-with-lease",
			pushes:  1,
		},
		{
			name:    "not a push",
			command: "git log --oneline | grep push",
			pushes:  0,
		},
	}
	s, err := NewScanner(nil)
	if err != nil {
		t.Fatal(err)
	}
	repoOf := func(string) string { return "Runware/runware-cli" }
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := s.planCommand(tc.command, dir, repoOf)
			var labels []string
			for _, pub := range p.texts {
				labels = append(labels, pub.label)
			}
			if strings.Join(labels, ",") != strings.Join(tc.labels, ",") {
				t.Errorf("labels = %v, want %v", labels, tc.labels)
			}
			if len(p.pushes) != tc.pushes {
				t.Errorf("pushes = %d, want %d", len(p.pushes), tc.pushes)
			}
		})
	}
}

func TestGHPrivateTargetFromRemote(t *testing.T) {
	s, err := NewScanner(nil)
	if err != nil {
		t.Fatal(err)
	}
	repoOf := func(string) string { return shortRepo + "some-private-repo" }
	if p := s.planCommand(`gh pr create -t x -b "`+leak+`"`, "", repoOf); len(p.texts) != 0 {
		t.Errorf("texts for a private repository = %+v", p.texts)
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
	dir := gitRepo(t, "clean text", "feat: add b\n\n"+leak)
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

func TestHookAllowsCleanPush(t *testing.T) {
	dir := gitRepo(t, "clean text", "feat: add b")
	var stderr bytes.Buffer
	payload := `{"tool_name":"Bash","tool_input":{"command":"git push"},"cwd":` + jsonString(dir) + `}`
	if code := run(context.Background(), []string{modeHook}, strings.NewReader(payload), &bytes.Buffer{}, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
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
	dir := tagRepo(t, "clean text")
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
