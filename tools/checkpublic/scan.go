package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Finding is one rule match with where it was found.
type Finding struct {
	Where   string
	Rule    string
	Message string
	Match   string
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

// ScanFile scans the content of one file, reported as file:line.
func (s *Scanner) ScanFile(file string, content []byte) []Finding {
	if excludedFile(file) || isBinary(content) {
		return nil
	}
	var out []Finding
	for i, line := range strings.Split(string(content), "\n") {
		out = append(out, s.findings(fmt.Sprintf("%s:%d", file, i+1), file, line)...)
	}
	return out
}

func isBinary(content []byte) bool {
	return bytes.IndexByte(content[:min(len(content), 8000)], 0) >= 0
}

func (s *Scanner) findings(where, file, line string) []Finding {
	var out []Finding
	for _, h := range s.ScanLine(file, line) {
		out = append(out, Finding{
			Where:   where,
			Rule:    h.rule,
			Message: h.message,
			Match:   h.match,
		})
	}
	return out
}

// ScanDiff scans the added lines of a unified diff. A diff from git log -p
// has lines starting with commitMarker; their findings are reported as
// "commit <sha> file:line".
func (s *Scanner) ScanDiff(diff string) []Finding {
	var out []Finding
	var file, commit string
	// The line counts left in the current hunk, so an added line that
	// starts with "++ " is not taken for a file header.
	newLine, oldLeft, newLeft := 0, 0, 0
	sc := bufio.NewScanner(strings.NewReader(diff))
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
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
			case strings.HasPrefix(line, " "):
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
	return out
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

func (g Git) run(ctx context.Context, args ...string) (string, error) {
	full := append([]string{"-C", g.Dir, "-c", "core.quotePath=false"}, args...)
	cmd := exec.CommandContext(ctx, "git", full...) //nolint:gosec // git with arguments built by this tool, no shell
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

func (g Git) revParse(ctx context.Context, rev string) (string, error) {
	out, err := g.run(ctx, "rev-parse", "--verify", "--quiet", rev+"^{commit}")
	return strings.TrimSpace(out), err
}

// MergeBase returns the merge-base of a and b.
func (g Git) MergeBase(ctx context.Context, a, b string) (string, error) {
	out, err := g.run(ctx, "merge-base", a, b)
	return strings.TrimSpace(out), err
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
}

// ScanRange scans what a range would publish: the added lines of the range's
// diff, the added lines of every commit in it (a line added and removed again
// is still published in the history), every commit message, and the branch
// name.
func (s *Scanner) ScanRange(ctx context.Context, g Git, r Range) ([]Finding, error) {
	var out []Finding
	if r.Branch != "" {
		out = append(out, s.ScanText("branch name", r.Branch)...)
	}

	diff, err := g.run(ctx, "diff", "--no-color", "--no-ext-diff", "--no-renames", "-U0", r.Base+"..."+r.Head)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	for _, f := range s.ScanDiff(diff) {
		seen[fileKey(f)] = struct{}{}
		out = append(out, f)
	}

	patches, err := g.run(ctx, "log", "--no-merges", "-p", "--no-color", "--no-ext-diff", "--no-renames", "-U0", "--format=%x00commit %H", r.Base+".."+r.Head)
	if err != nil {
		return nil, err
	}
	for _, f := range s.ScanDiff(patches) {
		key := fileKey(f)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, f)
	}

	msgs, err := g.run(ctx, "log", "--format=%H%x1f%B%x1e", r.Base+".."+r.Head)
	if err != nil {
		return nil, err
	}
	for rec := range strings.SplitSeq(msgs, "\x1e") {
		sha, body, ok := strings.Cut(strings.TrimLeft(rec, "\n"), "\x1f")
		if !ok {
			continue
		}
		out = append(out, s.ScanText("commit "+short(sha)+" message", strings.TrimRight(body, "\n"))...)
	}
	return out, nil
}

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
// merge-base with main; a tag on a commit remote already has adds none.
func (s *Scanner) ScanTag(ctx context.Context, g Git, remote, name, object string) ([]Finding, error) {
	out := s.ScanText("tag name", name)
	if typ, err := g.run(ctx, "cat-file", "-t", object); err == nil && strings.TrimSpace(typ) == "tag" {
		raw, err := g.run(ctx, "cat-file", "tag", object)
		if err != nil {
			return nil, err
		}
		if _, msg, ok := strings.Cut(raw, "\n\n"); ok {
			out = append(out, s.ScanText("tag "+name+" message", strings.TrimRight(msg, "\n"))...)
		}
	}
	commit, err := g.revParse(ctx, object)
	if err != nil {
		return out, nil //nolint:nilerr // a tag on a tree or a blob carries no commits
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
	base, err := g.DefaultBase(ctx, remote, commit)
	if err != nil {
		return nil, err
	}
	f, err := s.ScanRange(ctx, g, Range{
		Base: base,
		Head: commit,
	})
	if err != nil {
		return nil, err
	}
	return append(out, f...), nil
}

// hasRef reports whether a full ref name exists.
func (g Git) hasRef(ctx context.Context, ref string) bool {
	_, err := g.run(ctx, "show-ref", "--verify", "--quiet", ref)
	return err == nil
}
