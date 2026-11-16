package main

import (
	"context"
	"regexp"
	"slices"
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

// gitRepoOf returns the GitHub repository of dir whose variables hold the
// deny list: origin's when it is a Runware repository, else that of
// another remote that is one (upstream first), as when origin is a
// personal fork, else origin's.
func gitRepoOf(dir string) string {
	ctx := context.Background()
	g := Git{
		Dir: dir,
	}
	url := func(remote string) string {
		out, err := g.run(ctx, "remote", "get-url", remote)
		if err != nil {
			return ""
		}
		return githubRepo(out)
	}
	origin := url(remoteOriginName)
	if isRunwareRepo(origin) {
		return origin
	}
	list, err := g.run(ctx, "remote")
	if err != nil {
		return origin
	}
	remotes := strings.Fields(list)
	if i := slices.Index(remotes, remoteUpstreamName); i > 0 {
		remotes = slices.Concat([]string{remoteUpstreamName}, slices.Delete(remotes, i, i+1))
	}
	for _, remote := range remotes {
		if remote == remoteOriginName {
			continue
		}
		if repo := url(remote); isRunwareRepo(repo) {
			return repo
		}
	}
	return origin
}

const (
	remoteOriginName   = "origin"
	remoteUpstreamName = "upstream"
)

// isRunwareRepo reports whether repo, an owner/name from githubRepo, belongs
// to the Runware organization.
func isRunwareRepo(repo string) bool {
	owner, _, ok := strings.Cut(repo, "/")
	return ok && strings.EqualFold(owner, "runware")
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
