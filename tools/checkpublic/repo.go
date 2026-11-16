package main

import (
	"context"
	"regexp"
	"strings"
)

// isPrivateTarget reports whether repo, an owner/name from githubRepo, is a
// Runware repository that is not public. Publishing there is not this check's concern.
func (s *Scanner) isPrivateTarget(repo string) bool {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok || !strings.EqualFold(owner, "runware") {
		return false
	}
	return !s.IsPublicRepo(name)
}

// remoteURL matches a remote URL with a host: scheme://[user@]host[:port]/path
// or the scp-like [user@]host:path. Its groups are the host and the
// owner/name.
var remoteURL = regexp.MustCompile(`^(?:[a-z][a-z0-9+.-]*://)?(?:[^@/]+@)?([^@/:]+)(?::[0-9]*)?[:/]([^/]+/[^/]+?)(?:\.git)?/?$`)

// githubRepo returns the owner/name of a GitHub remote URL, or "". A host
// whose name contains "github" counts as GitHub, so an SSH host alias such
// as github-work (a Host entry in ~/.ssh/config) is recognized too.
func githubRepo(url string) string {
	m := remoteURL.FindStringSubmatch(strings.TrimSpace(url))
	if len(m) < 3 || !strings.Contains(strings.ToLower(m[1]), "github") {
		return ""
	}
	return m[2]
}

// gitRepoOf reads the GitHub repository of dir from its origin remote.
func gitRepoOf(dir string) string {
	out, err := Git{Dir: dir}.run(context.Background(), "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return githubRepo(out)
}

// gitCommonDir returns the common git directory of dir's clone, which every
// worktree of the clone shares, or "".
func gitCommonDir(dir string) string {
	out, err := Git{Dir: dir}.run(context.Background(), "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}
