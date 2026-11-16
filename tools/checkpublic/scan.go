package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
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

// run runs git with the attributes of the scanned tree disabled: a
// .gitattributes file is the branch's to write, and it could hide a file
// from the diff (-diff, binary) or select a filter, a textconv or a merge
// driver, which are commands git would run while --remerge-diff re-merges.
// Attributes are read from the empty tree instead (--attr-source), so only
// the user's own attribute files apply.
func (g Git) run(ctx context.Context, args ...string) (string, error) {
	if err := gitVersionOnce(); err != nil {
		return "", err
	}
	tree, err := emptyTree(ctx, g.Dir)
	if err != nil {
		return "", err
	}
	return g.exec(ctx, slices.Concat([]string{"--attr-source=" + tree}, args)...)
}

// exec runs git in g.Dir without the attribute source run sets.
func (g Git) exec(ctx context.Context, args ...string) (string, error) {
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

// Empty tree object IDs, which every repository of the object format has.
const (
	emptyTreeSHA1   = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"
	emptyTreeSHA256 = "6ef19b41225c5369f1c104d45d8d85efa9b057b53b14b4b9b939dd74decc5321"
)

// emptyTrees caches emptyTree by directory.
var emptyTrees sync.Map

// emptyTree returns the empty tree of dir's object format. Outside a
// repository it returns the SHA-1 one, which then has nothing to override.
func emptyTree(ctx context.Context, dir string) (string, error) {
	if tree, ok := emptyTrees.Load(dir); ok {
		return tree.(string), nil
	}
	tree, err := readEmptyTree(ctx, dir)
	if err == nil {
		emptyTrees.Store(dir, tree)
	}
	return tree, err
}

func readEmptyTree(ctx context.Context, dir string) (string, error) {
	out, err := Git{Dir: dir}.exec(ctx, "rev-parse", "--show-object-format")
	if err != nil {
		return emptyTreeSHA1, nil //nolint:nilerr // not a repository: the command run then fails on its own
	}
	switch format := strings.TrimSpace(out); format {
	case "sha1":
		return emptyTreeSHA1, nil
	case "sha256":
		return emptyTreeSHA256, nil
	default:
		return "", fmt.Errorf("unknown git object format %q", format)
	}
}

// minGitVersion is the oldest git with --attr-source (2.40). Run with an
// older one, the scan fails rather than read the branch's attributes.
var minGitVersion = [2]int{2, 40}

// ErrGitTooOld means the installed git cannot disable in-tree attributes.
var ErrGitTooOld = errors.New("git is too old to scan safely: checkpublic needs git 2.40 or newer (for --attr-source); upgrade git")

var gitVersionOnce = sync.OnceValue(func() error {
	out, err := exec.CommandContext(context.Background(), "git", "version").Output()
	if err != nil {
		return fmt.Errorf("git version: %w", err)
	}
	return gitVersionAtLeast(string(out), minGitVersion)
})

// gitVersionAtLeast checks the output of git version, such as "git version
// 2.39.5 (Apple Git-154)", against min.
func gitVersionAtLeast(out string, minVersion [2]int) error {
	fields := strings.Fields(out)
	if len(fields) < 3 {
		return fmt.Errorf("%w: cannot read %q", ErrGitTooOld, strings.TrimSpace(out))
	}
	parts := strings.SplitN(fields[2], ".", 3)
	if len(parts) < 2 {
		return fmt.Errorf("%w: cannot read %q", ErrGitTooOld, strings.TrimSpace(out))
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return fmt.Errorf("%w: cannot read %q", ErrGitTooOld, strings.TrimSpace(out))
	}
	if major < minVersion[0] || major == minVersion[0] && minor < minVersion[1] {
		return fmt.Errorf("%w: found %s", ErrGitTooOld, fields[2])
	}
	return nil
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
	// Remote, when set, names the remote a push goes to: commits it has
	// already (under refs/remotes/<Remote>/) are not scanned again.
	Remote string
}

// PushBase returns the base of what a push of head publishes. remoteSHA is
// what the remote branch points to, "" for a new branch. The base is the
// merge-base with that commit, or with remote/main when that is newer: after
// a rebase onto a newer main and a force-push, the old fork point would
// bring in main's commits that others wrote.
func (g Git) PushBase(ctx context.Context, remote, remoteSHA, head string) (string, error) {
	base := ""
	if remoteSHA != "" {
		mb, err := g.MergeBase(ctx, remoteSHA, head)
		if err != nil {
			return "", err
		}
		base = mb
	}
	mainBase, err := g.DefaultBase(ctx, remote, head)
	switch {
	case err != nil && base == "":
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
// driver and no textconv filter (both are commands a branch can configure in
// .gitattributes), a rename as a deletion and an addition, and every file
// as text, so a text file with a NUL byte is not skipped as binary (files
// that are binary by their extension are skipped when scanned).
var patchFlags = []string{"--no-color", "--no-ext-diff", "--no-textconv", "--no-renames", "--text", "-U0"}

// ScanRange scans what a range would publish: the added lines of the range's
// diff, the added lines of every commit in it (a line added and removed again
// is still published in the history), the path of every added file, every
// commit message, and the branch name. For a merge commit, what its
// resolution adds beyond the automatic merge of its parents counts as added
// (git's remerge diff); a clean merge adds nothing.
func (s *Scanner) ScanRange(ctx context.Context, g Git, r Range) ([]Finding, error) {
	var out []Finding
	if r.Branch != "" {
		out = append(out, s.ScanText("branch name", r.Branch)...)
	}

	diff, err := g.run(ctx, slices.Concat([]string{"diff"}, patchFlags, []string{r.Base + "..." + r.Head})...)
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	for _, f := range s.ScanDiff(diff) {
		seen[fileKey(f)] = struct{}{}
		out = append(out, f)
	}

	patches, err := g.run(ctx, slices.Concat([]string{"log", "--remerge-diff", "-p", "--format=%x00commit %H"}, patchFlags, []string{r.Base + ".." + r.Head}, r.notOnRemote())...)
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

	// git's remerge diff covers merges of two parents only. An octopus
	// merge is diffed against its first parent instead, which counts
	// everything its other parents bring in as added.
	octopus, err := g.run(ctx, append([]string{"rev-list", "--min-parents=3", r.Base + ".." + r.Head}, r.notOnRemote()...)...)
	if err != nil {
		return nil, err
	}
	var octopusPaths strings.Builder
	for sha := range strings.FieldsSeq(octopus) {
		d, err := g.run(ctx, slices.Concat([]string{"diff"}, patchFlags, []string{sha + "^1", sha})...)
		if err != nil {
			return nil, err
		}
		for _, f := range s.ScanDiff(commitMarker + sha + "\n" + d) {
			key := fileKey(f)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, f)
		}
		p, err := g.run(ctx, "diff", "--no-renames", "--diff-filter=A", "--name-only", sha+"^1", sha)
		if err != nil {
			return nil, err
		}
		octopusPaths.WriteString(p)
	}

	// The path of each file added in the range, or renamed (with
	// --no-renames a rename is an addition), or added by a merge
	// resolution, once.
	paths, err := g.run(ctx, append([]string{"log", "--remerge-diff", "--no-renames", "--diff-filter=A", "--name-only", "--format=", r.Base + ".." + r.Head}, r.notOnRemote()...)...)
	if err != nil {
		return nil, err
	}
	seenPath := map[string]struct{}{}
	for p := range strings.SplitSeq(paths+"\n"+octopusPaths.String(), "\n") {
		if _, ok := seenPath[p]; !ok && p != "" {
			seenPath[p] = struct{}{}
			out = append(out, s.ScanText("path of an added file", p)...)
		}
	}

	msgs, err := g.run(ctx, append([]string{"log", "--format=%H%x1f%B%x1e", r.Base + ".." + r.Head}, r.notOnRemote()...)...)
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

// objectTag is git's type of an annotated tag object.
const objectTag = "tag"

// maxTagChain bounds how many tag objects ScanTag follows, so a malformed
// repository cannot loop it.
const maxTagChain = 100

// ScanTag scans what pushing a tag to remote would publish: its name, the
// message of an annotated tag, and the commits it points to when remote does
// not have them yet. Those are scanned like a new branch, from the
// merge-base with main; a tag on a commit remote already has adds none. An
// annotated tag can point to another tag object, which the push publishes
// too: the name and message of each tag in the chain are scanned.
func (s *Scanner) ScanTag(ctx context.Context, g Git, remote, name, object string) ([]Finding, error) {
	out := s.ScanText("tag name", name)
	for range maxTagChain {
		typ, err := g.run(ctx, "cat-file", "-t", object)
		if err != nil || strings.TrimSpace(typ) != objectTag {
			break
		}
		raw, err := g.run(ctx, "cat-file", objectTag, object)
		if err != nil {
			return nil, err
		}
		header, msg, _ := strings.Cut(raw, "\n\n")
		target, tagName := "", name
		for line := range strings.SplitSeq(header, "\n") {
			key, value, _ := strings.Cut(line, " ")
			switch key {
			case "object":
				target = value
			case objectTag:
				if value != name {
					out = append(out, s.ScanText("tag object name", value)...)
				}
				tagName = value
			}
		}
		if msg != "" {
			out = append(out, s.ScanText("tag "+tagName+" message", strings.TrimRight(msg, "\n"))...)
		}
		if target == "" {
			break
		}
		object = target
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
