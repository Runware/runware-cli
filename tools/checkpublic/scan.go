package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Finding is one rule match with where it was found.
type Finding struct {
	Where   string
	Rule    string
	Message string
	Match   string
	// File is the file named in Where, which a public log must not show
	// either (see redactor).
	File string
	// Tag is the tag named in Where, for a finding in a tag's message,
	// which a public log must not show either.
	Tag string
}

// ScanText scans free text (a PR title, body or comment). label names it in
// findings, for example "PR body".
func (s *Scanner) ScanText(label, text string) []Finding {
	var out []Finding
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		where := label
		if len(lines) > 1 {
			where = fmt.Sprintf("%s line %d", label, i+1)
		}
		out = append(out, s.findings(where, "", line)...)
	}
	return out
}

func (s *Scanner) findings(where, file, line string) []Finding {
	var out []Finding
	for _, h := range s.ScanLine(file, line) {
		out = append(out, Finding{
			Where:   where,
			Rule:    h.rule,
			Message: h.message,
			Match:   h.match,
			File:    file,
		})
	}
	return out
}

// maxLineBytes is the longest line of git output ScanDiff reads. A longer
// line fails the scan rather than leave the rest of the diff unread.
var maxLineBytes = 64 << 20

// ErrLineTooLong means a line of git output was too long to scan.
var ErrLineTooLong = errors.New("a line of the diff is too long to scan, so the scan stopped; the change cannot be checked")

// ScanDiff scans the added lines of a unified diff. A diff from git log -p
// has lines starting with commitMarker; their findings are reported as
// "commit <sha> file:line". It fails when it cannot read the whole diff.
func (s *Scanner) ScanDiff(diff string) ([]Finding, error) {
	var out []Finding
	var file, commit string
	// The line counts left in the current hunk, so an added line that
	// starts with "++ " is not taken for a file header.
	newLine, oldLeft, newLeft := 0, 0, 0
	sc := bufio.NewScanner(strings.NewReader(diff))
	sc.Buffer(make([]byte, 0, min(1<<20, maxLineBytes)), maxLineBytes)
	for sc.Scan() {
		line := sc.Text()
		if oldLeft > 0 || newLeft > 0 {
			switch {
			case strings.HasPrefix(line, "+"):
				if file != "" && !excludedFile(file) {
					where := fmt.Sprintf("%s:%d", file, newLine)
					if commit != "" {
						where = fmt.Sprintf("commit %s %s", short(commit), where)
					}
					out = append(out, s.findings(where, file, line[1:])...)
				}
				newLine++
				newLeft--
			case strings.HasPrefix(line, "-"):
				oldLeft--
			case strings.HasPrefix(line, " ") || line == "":
				// An empty line is a blank context line printed with
				// diff.suppressBlankEmpty.
				newLine++
				oldLeft--
				newLeft--
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, commitMarker):
			commit = strings.TrimPrefix(line, commitMarker)
			file = ""
		case strings.HasPrefix(line, "diff --git "):
			file = ""
		case strings.HasPrefix(line, "+++ "):
			file = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(line, "+++ "), "b/"), "\t")
			if file == "/dev/null" {
				file = ""
			}
		case strings.HasPrefix(line, "@@ "):
			newLine, oldLeft, newLeft = parseHunk(line)
		}
	}
	if err := sc.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			return nil, fmt.Errorf("%w (limit %d bytes)", ErrLineTooLong, maxLineBytes)
		}
		return nil, err
	}
	return out, nil
}

// parseHunk returns the first new-file line number and the old and new line
// counts of a "@@ -a,b +c,d @@" header. A missing count is 1.
func parseHunk(header string) (start, oldCount, newCount int) {
	fields := strings.Fields(header)
	if len(fields) < 3 {
		return 0, 0, 0
	}
	_, oldCount = hunkRange(fields[1])
	start, newCount = hunkRange(fields[2])
	return start, oldCount, newCount
}

func hunkRange(r string) (start, count int) {
	r = strings.TrimLeft(r, "+-")
	first, n, hasCount := strings.Cut(r, ",")
	start, _ = strconv.Atoi(first)
	count = 1
	if hasCount {
		count, _ = strconv.Atoi(n)
	}
	return start, count
}

func short(sha string) string {
	if len(sha) > 10 {
		return sha[:10]
	}
	return sha
}

const commitMarker = "\x00commit "

// Git runs git in one directory.
type Git struct {
	Dir string
}

// gitConfig pins the settings that change the output this tool reads, so a
// user's configuration cannot hide added lines: paths printed unquoted, the
// patch of a root commit shown, blank context lines kept as " ", no context
// joined into a -U0 hunk, the a/ and b/ prefixes, every path from the top of
// the repository, no colour, and no signature check output.
var gitConfig = []string{
	"-c", "core.quotePath=false",
	"-c", "log.showRoot=true",
	"-c", "log.showSignature=false",
	"-c", "diff.suppressBlankEmpty=false",
	"-c", "diff.interHunkContext=0",
	"-c", "diff.noprefix=false",
	"-c", "diff.mnemonicPrefix=false",
	"-c", "diff.srcPrefix=a/",
	"-c", "diff.dstPrefix=b/",
	"-c", "diff.relative=false",
	"-c", "color.ui=false",
	"-c", "color.diff=false",
}

// run runs git in g.Dir with gitConfig.
func (g Git) run(ctx context.Context, args ...string) (string, error) {
	full := slices.Concat([]string{"-C", g.Dir}, gitConfig, args)
	cmd := exec.CommandContext(ctx, "git", full...) //nolint:gosec // git with arguments built by this tool, no shell
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

// emptyTree returns the empty tree of the repository's object format.
func (g Git) emptyTree(ctx context.Context) (string, error) {
	out, err := g.run(ctx, "hash-object", "-t", "tree", "--stdin")
	return strings.TrimSpace(out), err
}

func (g Git) revParse(ctx context.Context, rev string) (string, error) {
	out, err := g.run(ctx, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	return strings.TrimSpace(out), err
}

// ErrNoMergeBase means two commits have no common history, as an orphan
// branch or a notes ref has with main.
var ErrNoMergeBase = errors.New("no common history")

// MergeBase returns the merge-base of a and b, or ErrNoMergeBase.
func (g Git) MergeBase(ctx context.Context, a, b string) (string, error) {
	out, err := g.run(ctx, "merge-base", a, b)
	if exitErr := (*exec.ExitError)(nil); errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		// Both are commits (rev-parse checked) without a merge-base.
		if g.isCommit(ctx, a) && g.isCommit(ctx, b) {
			return "", fmt.Errorf("%s and %s: %w", short(a), short(b), ErrNoMergeBase)
		}
	}
	return strings.TrimSpace(out), err
}

// isCommit reports whether rev names a commit.
func (g Git) isCommit(ctx context.Context, rev string) bool {
	_, err := g.revParse(ctx, rev)
	return err == nil
}

// ErrNoMainRef means no remote main branch to compare against was found.
var ErrNoMainRef = errors.New("no origin/main to compare against: run git fetch origin, or pass -base")

// DefaultBase returns the merge-base of head with remote/main, falling back to
// origin/main.
func (g Git) DefaultBase(ctx context.Context, remote, head string) (string, error) {
	refs := []string{"origin/main"}
	if remote != "" && remote != "origin" {
		refs = []string{remote + "/main", "origin/main"}
	}
	for _, ref := range refs {
		if _, err := g.revParse(ctx, "refs/remotes/"+ref); err != nil {
			continue
		}
		return g.MergeBase(ctx, "refs/remotes/"+ref, head)
	}
	return "", ErrNoMainRef
}

// CurrentBranch returns the checked-out branch name, or "" when detached.
func (g Git) CurrentBranch(ctx context.Context) string {
	out, err := g.run(ctx, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// Range is a git range to scan: everything head adds on top of base.
type Range struct {
	Base   string
	Head   string
	Branch string
	// Remote, when set, names the remote a push goes to: commits it has
	// already (under refs/remotes/<Remote>/) are not scanned again.
	Remote string
}

// PushBase returns the base of what a push of head publishes. remoteSHA is
// what the remote branch points to, "" for a new branch. The base is the
// merge-base with that commit, or with remote/main when that is newer: after
// a rebase onto a newer main and a force-push, the old fork point would
// bring in main's commits that others wrote. It is "" when head shares no
// history with either (an orphan branch or a notes ref): then everything
// head has that the remote does not is new.
func (g Git) PushBase(ctx context.Context, remote, remoteSHA, head string) (string, error) {
	base := ""
	if remoteSHA != "" {
		mb, err := g.MergeBase(ctx, remoteSHA, head)
		if err != nil && !errors.Is(err, ErrNoMergeBase) {
			return "", err
		}
		base = mb
	}
	mainBase, err := g.DefaultBase(ctx, remote, head)
	switch {
	case errors.Is(err, ErrNoMergeBase):
		return base, nil
	case err != nil && remoteSHA == "":
		return "", err
	case err != nil:
		return base, nil //nolint:nilerr // without remote/main, the remote branch alone gives the base
	case base == "" || g.isAncestor(ctx, base, mainBase):
		return mainBase, nil
	}
	return base, nil
}

// isAncestor reports whether commit a is an ancestor of commit b.
func (g Git) isAncestor(ctx context.Context, a, b string) bool {
	_, err := g.run(ctx, "merge-base", "--is-ancestor", a, b)
	return err == nil
}

// notOnRemote returns the git log arguments that leave out the commits that
// r.Remote already has.
func (r Range) notOnRemote() []string {
	if r.Remote == "" || strings.ContainsAny(r.Remote, ":/") {
		return nil
	}
	return []string{"--not", "--remotes=" + r.Remote}
}

// patchFlags make git print plain patches: no colour, no external diff
// driver and no textconv filter, a rename as a deletion and an addition, and
// every file as text, so a file marked -diff or holding a NUL byte is not
// skipped as binary (files that are binary by their extension are skipped
// when scanned).
var patchFlags = []string{"--no-color", "--no-ext-diff", "--no-textconv", "--no-renames", "--text", "-U0"}

// ScanRange scans what a range would publish: the added lines of the range's
// diff, the added lines of every commit in it (a line added and removed again
// is still published in the history), the path of every added file, every
// commit message, and the branch name. For a merge commit, what its
// resolution adds beyond the automatic merge of its parents counts as added
// (git's remerge diff); a clean merge adds nothing. A merge of more than two
// parents fails the scan.
func (s *Scanner) ScanRange(ctx context.Context, g Git, r Range) ([]Finding, error) {
	var out []Finding
	if r.Branch != "" {
		out = append(out, s.ScanText("branch name", r.Branch)...)
	}

	// Without a base, the history is unrelated to what the remote has: the
	// whole tree is new, and so is every commit the remote does not have.
	diffRange := []string{r.Base + "..." + r.Head}
	logRange := []string{r.Base + ".." + r.Head}
	if r.Base == "" {
		tree, err := g.emptyTree(ctx)
		if err != nil {
			return nil, err
		}
		diffRange = []string{tree, r.Head}
		logRange = []string{r.Head}
	}
	logRange = append(logRange, r.notOnRemote()...)

	// The remerge diff covers merges of two parents only, so an octopus
	// merge fails the scan rather than going unchecked.
	octopus, err := g.run(ctx, slices.Concat([]string{"rev-list", "--min-parents=3", "-n", "1"}, logRange)...)
	if err != nil {
		return nil, err
	}
	if sha := strings.TrimSpace(octopus); sha != "" {
		return nil, fmt.Errorf("commit %s merges more than two parents, and such merges are not checked; merge the branches one at a time", short(sha))
	}

	diff, err := g.run(ctx, slices.Concat([]string{"diff"}, patchFlags, diffRange)...)
	if err != nil {
		return nil, err
	}
	diffFindings, err := s.ScanDiff(diff)
	if err != nil {
		return nil, err
	}
	patches, err := g.run(ctx, slices.Concat([]string{"log", "--remerge-diff", "-p", "--format=%x00commit %H"}, patchFlags, logRange)...)
	if err != nil {
		return nil, err
	}
	patchFindings, err := s.ScanDiff(patches)
	if err != nil {
		return nil, err
	}
	// With the remote's commits left out of the history, a line of the
	// range diff counts only when a commit the remote does not have adds
	// it: a branch started from a published branch does not answer for
	// what that branch already published.
	var unpublished map[string]struct{}
	if r.notOnRemote() != nil {
		unpublished = map[string]struct{}{}
		for _, f := range patchFindings {
			unpublished[fileKey(f)] = struct{}{}
		}
	}
	seen := map[string]struct{}{}
	for _, f := range diffFindings {
		key := fileKey(f)
		if _, ok := unpublished[key]; unpublished != nil && !ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, f)
	}
	for _, f := range patchFindings {
		key := fileKey(f)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, f)
	}

	// The path of each file added in the range, or renamed (with
	// --no-renames a rename is an addition), or added by a merge
	// resolution, once.
	paths, err := g.run(ctx, slices.Concat([]string{"log", "--remerge-diff", "--no-renames", "--diff-filter=A", "--name-only", "--format="}, logRange)...)
	if err != nil {
		return nil, err
	}
	seenPath := map[string]struct{}{}
	for p := range strings.SplitSeq(paths, "\n") {
		if _, ok := seenPath[p]; !ok && p != "" {
			seenPath[p] = struct{}{}
			out = append(out, s.ScanText("path of an added file", p)...)
		}
	}

	// git log -z ends each commit with a NUL, which a commit message made
	// with git commit cannot hold. A record that does not start with a
	// commit ID (a message written with a NUL by other means) is still
	// scanned, as a whole.
	msgs, err := g.run(ctx, slices.Concat([]string{"log", "-z", "--format=%H%n%B"}, logRange)...)
	if err != nil {
		return nil, err
	}
	for rec := range strings.SplitSeq(msgs, "\x00") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		label := "commit message"
		sha, body, ok := strings.Cut(rec, "\n")
		if ok && commitID.MatchString(sha) {
			label = "commit " + short(sha) + " message"
			rec = body
		}
		out = append(out, s.ScanText(label, strings.TrimRight(rec, "\n"))...)
	}
	return out, nil
}

// commitID matches a full commit ID, SHA-1 or SHA-256.
var commitID = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

// fileKey identifies a diff finding by file, rule and match, so a line found in
// the range diff is not reported again for the commit that added it.
func fileKey(f Finding) string {
	where := f.Where
	if strings.HasPrefix(where, "commit ") {
		parts := strings.SplitN(where, " ", 3)
		where = parts[len(parts)-1]
	}
	file, _, _ := strings.Cut(where, ":")
	return file + "\x00" + f.Rule + "\x00" + strings.ToLower(f.Match)
}

// ScanTag scans what pushing a tag to remote would publish: its name, the
// message of an annotated tag, and the commits it points to when remote does
// not have them yet. Those are scanned like a new branch, from the
// merge-base with main, leaving out what remote has; a tag on a commit
// remote already has adds none. A
// tag that does not lead to a commit, or that points to another annotated
// tag, fails the scan.
func (s *Scanner) ScanTag(ctx context.Context, g Git, remote, name, object string) ([]Finding, error) {
	out := s.ScanText("tag name", name)
	typ, err := g.run(ctx, "cat-file", "-t", object)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(typ) == "tag" {
		raw, err := g.run(ctx, "cat-file", "tag", object)
		if err != nil {
			return nil, err
		}
		header, msg, _ := strings.Cut(raw, "\n\n")
		if slices.Contains(strings.Split(header, "\n"), "type tag") {
			return nil, fmt.Errorf("tag %s points to another annotated tag, and nested annotated tags are not checked; push the inner tag on its own first", name)
		}
		if msg != "" {
			for _, f := range s.ScanText("tag "+name+" message", strings.TrimRight(msg, "\n")) {
				f.Tag = name
				out = append(out, f)
			}
		}
	}
	commit, err := g.revParse(ctx, object)
	if err != nil {
		return nil, fmt.Errorf("tag %s does not point to a commit, so it cannot be checked: %w", name, err)
	}
	remotes := "--remotes"
	if remote != "" {
		if _, err := g.run(ctx, "remote", "get-url", remote); err == nil {
			remotes = "--remotes=" + remote
		}
	}
	unpublished, err := g.run(ctx, "rev-list", "-n", "1", commit, "--not", remotes)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(unpublished) == "" {
		return out, nil
	}
	// The commits remote has already are not scanned again, on either
	// path (every commit is, when remote is not a configured remote).
	r := Range{
		Head:   commit,
		Remote: strings.TrimPrefix(strings.TrimPrefix(remotes, "--remotes"), "="),
	}
	r.Base, err = g.DefaultBase(ctx, remote, commit)
	if err != nil && !errors.Is(err, ErrNoMergeBase) {
		// Unrelated to main leaves Base empty: then everything the
		// remote does not have is new.
		return nil, err
	}
	f, err := s.ScanRange(ctx, g, r)
	if err != nil {
		return nil, err
	}
	return append(out, f...), nil
}
