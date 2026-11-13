// Command checkpublic finds text that must not be published in this public
// repository: references to private repositories, links to internal tools,
// internal hostnames, unpublished OpenAPI sections and credentials.
//
// Usage:
//
//	checkpublic range [-base REV] [-head REV] [-branch NAME]
//	checkpublic text [-label LABEL] [FILE|-]...
//	checkpublic pr [-event FILE]
//	checkpublic files [PATH]...
//	checkpublic hook      < Claude Code PreToolUse payload
//	checkpublic prepush REMOTE URL   < git pre-push ref lines
//
// It prints each finding with where it was found and the rule that fired, and
// exits 1 when there are findings (2 in hook mode, which blocks the tool call).
//
// The pr mode checks a pull request in GitHub Actions: its range, branch name,
// title and body, all read from the event file at GITHUB_EVENT_PATH. The
// runner prints a step's env in its public log, so the author's text must not
// reach the tool through env.
//
// Names that must not be published but cannot be written in this repository
// either live in the CHECKPUBLIC_DENY repository variable, one regular
// expression per line. Outside CI the tool reads it with the user's gh login
// and caches it for a day (see deny.go); the CHECKPUBLIC_DENY environment
// variable can add more. CI does not read it, because its log is public.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	exitFindings = 1
	exitError    = 3
	// exitBlock is the exit code that makes Claude Code and Codex block a tool call
	// and show stderr to the model.
	exitBlock = 2
)

// options are the flags every mode accepts.
type options struct {
	dir     string
	redact  bool
	summary string
}

// modeHook is the mode agent hooks run: it reads a PreToolUse payload.
const modeHook = "hook"

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		logln(stderr, "usage: checkpublic range|text|files|pr|hook|prepush [flags]")
		return exitError
	}
	mode := args[0]
	fs := flag.NewFlagSet("checkpublic "+mode, flag.ContinueOnError)
	fs.SetOutput(stderr)
	inCI := os.Getenv("GITHUB_ACTIONS") == "true"
	var o options
	fs.StringVar(&o.dir, "C", ".", "repository directory")
	fs.BoolVar(&o.redact, "redact", inCI, "do not print matched text (default on in GitHub Actions, whose logs are public)")
	defaultSummary := ""
	if inCI {
		defaultSummary = os.Getenv("GITHUB_STEP_SUMMARY")
	}
	fs.StringVar(&o.summary, "summary", defaultSummary, "append a Markdown report to this file")
	base := fs.String("base", "", "range mode: base revision (default: merge-base of head with origin/main)")
	head := fs.String("head", "HEAD", "range mode: head revision")
	branch := fs.String("branch", "", "range mode: branch name to check (default: the checked-out branch)")
	label := fs.String("label", "text", "text mode: what the text is, for example \"PR body\"")
	event := fs.String("event", os.Getenv("GITHUB_EVENT_PATH"), "pr mode: the pull_request event JSON file")
	if err := fs.Parse(args[1:]); err != nil {
		return exitError
	}

	s, err := NewScanner(nil)
	if err != nil {
		logln(stderr, "checkpublic:", err)
		return exitError
	}
	// Deny list warnings are held until the report is out, so a blocked
	// hook leads with its findings rather than with a warning.
	var warnings bytes.Buffer
	defer func() {
		_, _ = stderr.Write(warnings.Bytes())
	}()
	s.loadDeny = func() []string {
		return newDenySource(inCI, o.dir, &warnings).patterns(ctx)
	}
	g := Git{
		Dir: o.dir,
	}

	var findings []Finding
	switch mode {
	case "range":
		findings, err = scanRangeMode(ctx, s, g, *base, *head, *branch)
	case "text":
		findings, err = scanTextMode(s, *label, fs.Args(), stdin)
	case wordFiles:
		findings, err = scanFilesMode(ctx, s, g, fs.Args())
	case "prepush":
		findings, err = s.prePush(ctx, g, fs.Args(), stdin)
	case "pr":
		return runPRMode(ctx, s, g, *event, o, stdout, stderr)
	case modeHook:
		return runHookMode(ctx, s, stdin, stderr)
	default:
		logf(stderr, "checkpublic: unknown mode %q\n", mode)
		return exitError
	}
	if err != nil {
		logln(stderr, "checkpublic:", err)
		return exitError
	}
	heading := "diff, commit messages and branch name"
	switch mode {
	case "text":
		heading = *label
	case wordFiles:
		heading = wordFiles
	}
	return finish(o, []section{{
		heading:  heading,
		findings: findings,
	}}, stdout, stderr)
}

// section is one part of a report with its own summary heading.
type section struct {
	heading  string
	findings []Finding
}

// finish reports the findings, appends the summary and returns the exit code.
func finish(o options, sections []section, stdout, stderr io.Writer) int {
	total := 0
	for _, sec := range sections {
		report(stdout, sec.findings, o.redact)
		total += len(sec.findings)
		if o.summary != "" {
			if err := writeSummary(o.summary, sec.heading, sec.findings); err != nil {
				logln(stderr, "checkpublic:", err)
			}
		}
	}
	if total > 0 {
		logf(stderr, "checkpublic: %d finding(s). This repository is public: rewrite the text, or for a false positive add `%s` to the line with the reason.\n", total, allowMarker)
		return exitFindings
	}
	return 0
}

// prEvent is the part of a GitHub pull_request event the pr mode reads.
type prEvent struct {
	PullRequest prPayload `json:"pull_request"`
}

type prPayload struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Base  prRef  `json:"base"`
	Head  prRef  `json:"head"`
}

type prRef struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

// runPRMode checks a pull request: its range, branch name, title and body.
func runPRMode(ctx context.Context, s *Scanner, g Git, eventPath string, o options, stdout, stderr io.Writer) int {
	if eventPath == "" {
		logln(stderr, "checkpublic pr: no event file: set GITHUB_EVENT_PATH or pass -event")
		return exitError
	}
	b, err := os.ReadFile(eventPath) //nolint:gosec // the event file GitHub Actions names
	if err != nil {
		logln(stderr, "checkpublic pr:", err)
		return exitError
	}
	var ev prEvent
	if err := json.Unmarshal(b, &ev); err != nil {
		logln(stderr, "checkpublic pr: read event:", err)
		return exitError
	}
	pr := ev.PullRequest
	if pr.Base.SHA == "" || pr.Head.SHA == "" {
		logln(stderr, "checkpublic pr: the event has no pull_request base and head")
		return exitError
	}
	rangeFindings, err := s.ScanRange(ctx, g, Range{
		Base:   pr.Base.SHA,
		Head:   pr.Head.SHA,
		Branch: pr.Head.Ref,
	})
	if err != nil {
		logln(stderr, "checkpublic pr:", err)
		return exitError
	}
	return finish(o, []section{
		{
			heading:  "diff, commit messages and branch name",
			findings: rangeFindings,
		},
		{
			heading:  "PR title",
			findings: s.ScanText("PR title", pr.Title),
		},
		{
			heading:  "PR body",
			findings: s.ScanText("PR body", pr.Body),
		},
	}, stdout, stderr)
}

func scanRangeMode(ctx context.Context, s *Scanner, g Git, base, head, branch string) ([]Finding, error) {
	headSHA, err := g.revParse(ctx, head)
	if err != nil {
		return nil, err
	}
	if base == "" {
		base, err = g.DefaultBase(ctx, "", headSHA)
		if err != nil {
			return nil, err
		}
	}
	if branch == "" && head == "HEAD" {
		branch = g.CurrentBranch(ctx)
	}
	return s.ScanRange(ctx, g, Range{
		Base:   base,
		Head:   headSHA,
		Branch: branch,
	})
}

func scanTextMode(s *Scanner, label string, files []string, stdin io.Reader) ([]Finding, error) {
	if len(files) == 0 {
		files = []string{"-"}
	}
	var out []Finding
	for _, f := range files {
		var b []byte
		var err error
		if f == "-" {
			b, err = io.ReadAll(stdin)
		} else {
			b, err = os.ReadFile(f) //nolint:gosec // a file named on the command line
		}
		if err != nil {
			return nil, err
		}
		out = append(out, s.ScanText(label, string(b))...)
	}
	return out, nil
}

func scanFilesMode(ctx context.Context, s *Scanner, g Git, paths []string) ([]Finding, error) {
	if len(paths) == 0 {
		out, err := g.run(ctx, "ls-files", "-z")
		if err != nil {
			return nil, err
		}
		for p := range strings.SplitSeq(out, "\x00") {
			if p != "" {
				paths = append(paths, p)
			}
		}
	}
	var out []Finding
	for _, p := range paths {
		b, err := os.ReadFile(filepath.Join(g.Dir, p)) //nolint:gosec // a tracked file, or one named on the command line
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, err
		}
		out = append(out, s.ScanFile(filepath.ToSlash(p), b)...)
	}
	return out, nil
}

func runHookMode(ctx context.Context, s *Scanner, stdin io.Reader, stderr io.Writer) int {
	payload, err := io.ReadAll(stdin)
	if err != nil {
		logln(stderr, "checkpublic hook:", err)
		return exitFindings
	}
	findings, err := s.runHook(ctx, payload, gitRepoOf)
	if ue, ok := errors.AsType[*UncheckableError](err); ok {
		logf(stderr, "checkpublic: blocked. This command publishes to GitHub, or may, in a form the check cannot read completely: %s.\n", ue.Reason)
		logln(stderr, "Write the body to a file in a separate call, then post with a simple `gh ... --body-file <file>` (no $( ), no cd, no variables). Run a git push in its own call.")
		return exitBlock
	}
	if err != nil {
		// A non-blocking error: the git pre-push hook and CI still check.
		logln(stderr, "checkpublic hook: could not check this command:", err)
		if len(findings) == 0 {
			return exitFindings
		}
	}
	if len(findings) == 0 {
		return 0
	}
	logln(stderr, "checkpublic: blocked. This repository is public, and this command or tool call would publish:")
	report(stderr, findings, false)
	logf(stderr, "Rewrite the text (or the commits, with neutral messages) and run the command again. A reference to a repository is allowed only when the repository is public (tools/checkpublic/public-repos.txt). For a false positive in a file, add `%s` to the line with the reason.\n", allowMarker)
	return exitBlock
}

// prePush reads git's pre-push ref lines and scans each pushed range and tag.
func (s *Scanner) prePush(ctx context.Context, g Git, args []string, stdin io.Reader) ([]Finding, error) {
	remote := ""
	if len(args) > 0 {
		remote = args[0]
	}
	if len(args) > 1 && s.isPrivateTarget(githubRepo(args[1])) {
		return nil, nil
	}
	var out []Finding
	sc := bufio.NewScanner(stdin)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 4 {
			continue
		}
		localSHA, remoteRef, remoteSHA := fields[1], fields[2], fields[3]
		if isZeroSHA(localSHA) {
			continue // a deletion publishes nothing
		}
		if name, ok := strings.CutPrefix(remoteRef, "refs/tags/"); ok {
			f, err := s.ScanTag(ctx, g, remote, name, localSHA)
			if err != nil {
				return nil, err
			}
			out = append(out, f...)
			continue
		}
		known := ""
		if !isZeroSHA(remoteSHA) {
			if _, err := g.revParse(ctx, remoteSHA); err == nil {
				known = remoteSHA
			}
		}
		base, err := g.PushBase(ctx, remote, known, localSHA)
		if err != nil {
			return nil, err
		}
		f, err := s.ScanRange(ctx, g, Range{
			Base:   base,
			Head:   localSHA,
			Branch: strings.TrimPrefix(remoteRef, "refs/heads/"),
			Remote: remote,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, f...)
	}
	return out, sc.Err()
}

func isZeroSHA(sha string) bool {
	return strings.Trim(sha, "0") == ""
}

func report(w io.Writer, findings []Finding, redact bool) {
	for _, f := range findings {
		if redact {
			logf(w, "%s: [%s] %s (matched text hidden, %d characters)\n", f.Where, f.Rule, f.Message, len(f.Match))
			continue
		}
		logf(w, "%s: [%s] %s: %q\n", f.Where, f.Rule, f.Message, f.Match)
	}
}

// writeSummary appends a Markdown report. It never includes the matched text:
// the summary of a public repository's workflow run is public.
func writeSummary(path, heading string, findings []Finding) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "### Public content: %s\n\n", strings.ReplaceAll(heading, "\n", " "))
	if len(findings) == 0 {
		b.WriteString("No findings.\n")
	} else {
		fmt.Fprintf(&b, "%d finding(s). The matched text is not shown because this log is public.\n\n| Where | Rule | Problem |\n| --- | --- | --- |\n", len(findings))
		for _, fd := range findings {
			fmt.Fprintf(&b, "| `%s` | %s | %s |\n", strings.ReplaceAll(fd.Where, "`", "'"), fd.Rule, strings.ReplaceAll(fd.Message, "|", "/"))
		}
	}
	_, err = f.WriteString(b.String())
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// logf and logln write a message for the person running the tool. A failed
// write to stderr has nowhere to be reported.
func logf(w io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(w, format, a...)
}

func logln(w io.Writer, a ...any) {
	_, _ = fmt.Fprintln(w, a...)
}
