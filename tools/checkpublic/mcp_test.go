package main

import (
	"context"
	"encoding/json"
	"maps"
	"testing"
)

// keyBody is the argument that holds a comment or description.
const (
	keyBody      = "body"
	keyRepo      = "repo"
	keyOwner     = "owner"
	ownerRunware = "Runware"
	branchMain   = "main"
)

// privateTarget is a Runware repository that is not public, built at run time
// (see rules_test.go).
var privateTarget = ownerRunware + "/some-private-repo"

// bashPayload is a Claude Code PreToolUse payload for a Bash command.
func bashPayload(t *testing.T, command, dir string) []byte {
	t.Helper()
	input, err := json.Marshal(hookToolInput{
		Command: command,
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(hookInput{
		ToolName:  "Bash",
		ToolInput: input,
		Cwd:       dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	return payload
}

// TestHookCodexBashPayload checks the payload Codex sends for a shell command:
// the same tool_name, tool_input.command and cwd as Claude Code, with more
// fields around them.
func TestHookCodexBashPayload(t *testing.T) {
	dir := gitRepo(t, "clean text", "feat: add b\n\n"+leak)
	s, err := NewScanner(nil)
	if err != nil {
		t.Fatal(err)
	}
	payload := `{"session_id":"s","turn_id":"t","transcript_path":null,"cwd":` + jsonString(dir) +
		`,"hook_event_name":"PreToolUse","model":"m","permission_mode":"default","tool_name":"Bash",` +
		`"tool_input":{"command":"git push -u origin feature"},"tool_use_id":"exec-1"}`
	f, err := s.runHook(context.Background(), []byte(payload), gitRepoOf)
	if err != nil {
		t.Fatal(err)
	}
	if len(f) != 1 {
		t.Fatalf("findings = %+v, want one in the commit message", f)
	}
}

type toolNameCase struct {
	name string
	want bool
}

func TestIsGitHubWriteTool(t *testing.T) {
	cases := []toolNameCase{
		{
			name: "mcp__codex_apps__github__add_review_to_pr",
			want: true,
		},
		{
			name: "mcp__codex_apps__github__create_pull_request",
			want: true,
		},
		{
			name: "mcp__codex_apps__github__update_issue_comment",
			want: true,
		},
		{
			name: "mcp__codex_apps__github__reply_to_review_comment",
			want: true,
		},
		{
			name: "mcp__github__add_issue_comment",
			want: true,
		},
		{
			name: "mcp__github__pull_request_review_write",
			want: true,
		},
		{
			name: "mcp__plugin_github_github__create_issue",
			want: true,
		},
		{
			name: "mcp__github__push_files",
			want: true,
		},
		{
			name: "mcp__github__create_or_update_file",
			want: true,
		},
		{
			name: "mcp__claude_ai_GitHub__create_branch",
			want: true,
		},
		{
			name: "mcp__codex_apps__github__fetch_pr_comments",
			want: false,
		},
		{
			name: "mcp__github__get_pull_request",
			want: false,
		},
		{
			name: "mcp__codex_apps__github__request_pull_request_reviewers",
			want: false,
		},
		{
			name: "mcp__notion__create_comment",
			want: false,
		},
		{
			name: "Bash",
			want: false,
		},
	}
	for _, tc := range cases {
		if got := isGitHubWriteTool(tc.name); got != tc.want {
			t.Errorf("isGitHubWriteTool(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

type toolCallCase struct {
	name   string
	tool   string
	input  map[string]any
	want   int
	wantAt string
}

func TestHookGitHubToolCall(t *testing.T) {
	s, err := NewScanner(nil)
	if err != nil {
		t.Fatal(err)
	}
	review := map[string]any{
		"action":    "COMMENT",
		"review":    "looks fine",
		"commit_id": leak,
		"file_comments": []any{
			map[string]any{
				"path":  "a.go",
				"line":  1,
				keyBody: leak,
			},
		},
	}
	cases := []toolCallCase{
		{
			name:   "Codex review comment on a public repository",
			tool:   "mcp__codex_apps__github__add_review_to_pr",
			input:  withRepo(review, "repo_full_name", "Runware/runware-cli"),
			want:   1,
			wantAt: "mcp__codex_apps__github__add_review_to_pr file_comments[0].body",
		},
		{
			name:  "Codex review on a private Runware repository",
			tool:  "mcp__codex_apps__github__add_review_to_pr",
			input: withRepo(review, "repo_full_name", privateTarget),
			want:  0,
		},
		{
			name: "Codex pull request title",
			tool: "mcp__codex_apps__github__update_pull_request",
			input: map[string]any{
				"repository_full_name": "Runware/runware-cli",
				"pr_number":            1,
				"title":                leak,
			},
			want:   1,
			wantAt: "mcp__codex_apps__github__update_pull_request title",
		},
		{
			name: "Claude Code GitHub server with owner and repo",
			tool: "mcp__github__create_pull_request",
			input: map[string]any{
				keyOwner: ownerRunware,
				keyRepo:  "runware-cli",
				"title":  "feat: x",
				keyBody:  leak,
				"head":   "feature",
			},
			want:   1,
			wantAt: "mcp__github__create_pull_request body",
		},
		{
			name: "Claude Code GitHub server, private repository",
			tool: "mcp__github__add_issue_comment",
			input: map[string]any{
				keyOwner: ownerRunware,
				keyRepo:  "some-private-repo",
				keyBody:  leak,
			},
			want: 0,
		},
		{
			name: "file pushed straight to a branch",
			tool: "mcp__github__push_files",
			input: map[string]any{
				keyOwner:  ownerRunware,
				keyRepo:   "runware-cli",
				"branch":  "main",
				"message": "docs: x",
				"files": []any{
					map[string]any{
						"path":    "a.md",
						"content": "intro\n" + leak,
					},
				},
			},
			want:   1,
			wantAt: "mcp__github__push_files files[0].content line 2",
		},
		{
			name: "branch created with a private name",
			tool: "mcp__claude_ai_GitHub__create_branch",
			input: map[string]any{
				keyOwner:      ownerRunware,
				keyRepo:       repoCLI,
				"branch":      "fix/" + clusterHost,
				"from_branch": branchMain,
			},
			want:   1,
			wantAt: "mcp__claude_ai_GitHub__create_branch branch",
		},
		{
			name: "commit message of a file update",
			tool: "mcp__github__create_or_update_file",
			input: map[string]any{
				keyOwner:  ownerRunware,
				keyRepo:   repoCLI,
				"path":    "a.md",
				"content": "clean",
				"message": "docs: " + leak,
				"branch":  branchMain,
			},
			want:   1,
			wantAt: "mcp__github__create_or_update_file message",
		},
		{
			name: "unknown repository is checked",
			tool: "mcp__github__add_issue_comment",
			input: map[string]any{
				keyBody: leak,
			},
			want: 1,
		},
		{
			name: "read tool passes",
			tool: "mcp__github__get_pull_request",
			input: map[string]any{
				keyOwner: ownerRunware,
				keyRepo:  "runware-cli",
				keyBody:  leak,
			},
			want: 0,
		},
		{
			name: "other server passes",
			tool: "mcp__notion__create_comment",
			input: map[string]any{
				keyBody: leak,
			},
			want: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input, err := json.Marshal(tc.input)
			if err != nil {
				t.Fatal(err)
			}
			payload, err := json.Marshal(hookInput{
				ToolName:  tc.tool,
				ToolInput: input,
				Cwd:       t.TempDir(),
			})
			if err != nil {
				t.Fatal(err)
			}
			f, err := s.runHook(context.Background(), payload, gitRepoOf)
			if err != nil {
				t.Fatal(err)
			}
			if len(f) != tc.want {
				t.Fatalf("findings = %+v, want %d", f, tc.want)
			}
			if tc.wantAt != "" && f[0].Where != tc.wantAt+" line 1" && f[0].Where != tc.wantAt {
				t.Errorf("finding at %q, want %q", f[0].Where, tc.wantAt)
			}
		})
	}
}

// withRepo returns a copy of args with key set to repo.
func withRepo(args map[string]any, key, repo string) map[string]any {
	out := make(map[string]any, len(args)+1)
	maps.Copy(out, args)
	out[key] = repo
	return out
}
