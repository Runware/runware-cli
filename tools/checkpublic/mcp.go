package main

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// githubWriteVerbs and githubWriteObjects are the words of an MCP tool name
// that publishes a title, body, comment or file on GitHub:
// create_pull_request, add_issue_comment, pull_request_review_write,
// add_review_to_pr, push_files, create_or_update_file and so on.
// A tool needs one word from each set.
var (
	githubWriteVerbs = map[string]struct{}{
		"add":     {},
		"create":  {},
		"dismiss": {},
		"edit":    {},
		"merge":   {},
		"post":    {},
		wordPush:  {},
		"reply":   {},
		"submit":  {},
		"update":  {},
		"write":   {},
	}
	githubWriteObjects = map[string]struct{}{
		"branch":   {},
		"comment":  {},
		"comments": {},
		"file":     {},
		wordFiles:  {},
		wordIssue:  {},
		"pr":       {},
		"pull":     {},
		"review":   {},
	}
)

// toolTextFields are the arguments of a GitHub write tool that publish text,
// at any depth: a review's file_comments[].body, for example.
var toolTextFields = map[string]struct{}{
	"body":           {},
	"branch":         {},
	"comment":        {},
	"commit_message": {},
	"commit_title":   {},
	"content":        {},
	"message":        {},
	"review":         {},
	"title":          {},
}

// toolRepoFields are the arguments that name the target repository as
// owner/name, in the order they are tried.
var toolRepoFields = []string{
	"repo_full_name",
	"repository_full_name",
	"full_name",
	"repository",
	"repo",
}

// isGitHubWriteTool reports whether an MCP tool name (mcp__<server>__<tool>,
// such as mcp__github__create_pull_request in Claude Code or
// mcp__codex_apps__github__add_review_to_pr in Codex) posts text to GitHub.
func isGitHubWriteTool(name string) bool {
	n := strings.ToLower(name)
	if !strings.HasPrefix(n, "mcp__") || !strings.Contains(n, "github") {
		return false
	}
	tool := n[strings.LastIndex(n, "__")+2:]
	verb, object := false, false
	for _, w := range strings.FieldsFunc(tool, func(r rune) bool {
		return r == '_' || r == '-'
	}) {
		if _, ok := githubWriteVerbs[w]; ok {
			verb = true
		}
		if _, ok := githubWriteObjects[w]; ok {
			object = true
		}
	}
	return verb && object
}

// toolRepo returns the owner/name a GitHub tool call targets, or "" when its
// arguments do not say.
func toolRepo(args map[string]any) string {
	owner, _ := args["owner"].(string)
	repo, _ := args["repo"].(string)
	if owner != "" && repo != "" && !strings.Contains(repo, "/") {
		return owner + "/" + repo
	}
	for _, k := range toolRepoFields {
		if v, ok := args[k].(string); ok && strings.Contains(v, "/") {
			return v
		}
	}
	return ""
}

// scanToolCall returns the findings in the text a GitHub write tool call would
// publish. A call to a private Runware repository is not this check's concern.
func (s *Scanner) scanToolCall(name string, input json.RawMessage) ([]Finding, error) {
	var args map[string]any
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, fmt.Errorf("read %s arguments: %w", name, err)
	}
	if s.isPrivateTarget(toolRepo(args)) {
		return nil, nil
	}
	var out []Finding
	collectTexts(args, "", false, func(path, text string) {
		out = append(out, s.ScanText(name+" "+path, text)...)
	})
	return out, nil
}

// collectTexts walks v in a stable order and passes each string to publish to
// found with its path: every string when all is set, otherwise only those
// under a key in toolTextFields.
func collectTexts(v any, path string, all bool, found func(path, text string)) {
	switch x := v.(type) {
	case string:
		if all {
			found(path, x)
		}
	case []any:
		for i, e := range x {
			collectTexts(e, fmt.Sprintf("%s[%d]", path, i), all, found)
		}
	case map[string]any:
		for _, k := range slices.Sorted(maps.Keys(x)) {
			p := k
			if path != "" {
				p = path + "." + k
			}
			_, isText := toolTextFields[k]
			collectTexts(x[k], p, all || isText, found)
		}
	}
}
