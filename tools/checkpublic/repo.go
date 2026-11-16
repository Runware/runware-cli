package main

import (
	"context"
	"regexp"
	"strings"
)

// isPrivateTarget reports whether repo, an owner/name from githubRepo, is a
// Runware repository that is not public. Publishing there is not this check's concern.
func (s *Scanner) isPrivateTarget(repo string) bool {
	_, name, _ := strings.Cut(repo, "/")
	return isRunwareRepo(repo) && !s.IsPublicRepo(name)
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

// gitRepoOf returns the GitHub repository of dir's origin remote, whose
// variables hold the deny list, or "".
func gitRepoOf(dir string) string {
	out, err := Git{Dir: dir}.run(context.Background(), "remote", "get-url", remoteOriginName)
	if err != nil {
		return ""
	}
	return githubRepo(out)
}

const remoteOriginName = "origin"

// isRunwareRepo reports whether repo, an owner/name from githubRepo, belongs
// to the Runware organization.
func isRunwareRepo(repo string) bool {
	owner, _, ok := strings.Cut(repo, "/")
	return ok && strings.EqualFold(owner, "runware")
}
