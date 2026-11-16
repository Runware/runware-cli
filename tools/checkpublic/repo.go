package main

import (
	"context"
	"regexp"
	"strings"
)

// isPrivateTarget reports whether owner/name is a Runware repository that is
// not public. Publishing there is not this check's concern.
func (s *Scanner) isPrivateTarget(repo string) bool {
	repo = strings.TrimSuffix(repo, ".git")
	if i := strings.LastIndex(repo, "github.com/"); i >= 0 {
		repo = repo[i+len("github.com/"):]
	}
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || !strings.EqualFold(owner, "runware") {
		return false
	}
	return !s.IsPublicRepo(name)
}

var remoteURL = regexp.MustCompile(`github\.com[:/]([^/]+/[^/]+?)(?:\.git)?/?$`)

// githubRepo returns the owner/name of a GitHub remote URL, or "".
func githubRepo(url string) string {
	m := remoteURL.FindStringSubmatch(strings.TrimSpace(url))
	if m == nil {
		return ""
	}
	return m[1]
}

// gitRepoOf reads the GitHub repository of dir from its origin remote.
func gitRepoOf(dir string) string {
	out, err := Git{Dir: dir}.run(context.Background(), "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return githubRepo(out)
}
