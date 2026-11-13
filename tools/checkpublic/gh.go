package main

import (
	"regexp"
	"strings"
)

// flagKind says what a flag's value is.
type flagKind int

const (
	// flagValue takes a value that is not published.
	flagValue flagKind = iota
	// flagText takes text that is published.
	flagText
	// flagFile takes a file, or - for stdin, whose content is published.
	flagFile
)

// flagSpec is one flag that takes a value. Flags not listed are taken as
// booleans: a value they take is then read as a positional argument, so a
// missing entry makes the hook read more, never less.
type flagSpec struct {
	long  string
	short byte
	kind  flagKind
}

// flagVal is one flag found on a command line.
type flagVal struct {
	spec  flagSpec
	value string
	dyn   bool
}

// parsedArgs is a command line split the way gh's flag parser splits it.
type parsedArgs struct {
	flags  []flagVal
	pos    []string
	posDyn []bool
}

// parseFlags splits args as pflag does: --name value, --name=value, -x value,
// -xvalue, -x=value and boolean shorthands grouped before a value flag
// (-wbtext), with -- ending the flags.
func parseFlags(args []string, dyn []bool, specs []flagSpec) parsedArgs {
	var p parsedArgs
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			p.pos = append(p.pos, args[i+1:]...)
			p.posDyn = append(p.posDyn, dyn[i+1:]...)
			return p
		case strings.HasPrefix(a, "--"):
			name, value, hasValue := strings.Cut(a[2:], "=")
			spec, ok := findLong(specs, name)
			if !ok {
				continue
			}
			d := dyn[i]
			if !hasValue && i+1 < len(args) {
				i++
				value, d = args[i], dyn[i]
			}
			p.flags = append(p.flags, flagVal{
				spec:  spec,
				value: value,
				dyn:   d,
			})
		case strings.HasPrefix(a, "-") && len(a) > 1:
			for j := 1; j < len(a); j++ {
				spec, ok := findShort(specs, a[j])
				if !ok {
					continue
				}
				value, d := strings.TrimPrefix(a[j+1:], "="), dyn[i]
				if j+1 == len(a) && i+1 < len(args) {
					i++
					value, d = args[i], dyn[i]
				}
				p.flags = append(p.flags, flagVal{
					spec:  spec,
					value: value,
					dyn:   d,
				})
				break
			}
		default:
			p.pos = append(p.pos, a)
			p.posDyn = append(p.posDyn, dyn[i])
		}
	}
	return p
}

func findLong(specs []flagSpec, name string) (flagSpec, bool) {
	for _, s := range specs {
		if s.long == name {
			return s, true
		}
	}
	return flagSpec{}, false
}

func findShort(specs []flagSpec, c byte) (flagSpec, bool) {
	for _, s := range specs {
		if s.short != 0 && s.short == c {
			return s, true
		}
	}
	return flagSpec{}, false
}

// newFlag returns a flagSpec.
func newFlag(long string, short byte, kind flagKind) flagSpec {
	return flagSpec{
		long:  long,
		short: short,
		kind:  kind,
	}
}

var (
	flagRepo      = newFlag("repo", 'R', flagValue)
	flagTitle     = newFlag("title", 't', flagText)
	flagBody      = newFlag("body", 'b', flagText)
	flagBodyFile  = newFlag("body-file", 'F', flagFile)
	flagComment   = newFlag("comment", 'c', flagText)
	flagNotes     = newFlag("notes", 'n', flagText)
	flagNotesFile = newFlag("notes-file", 'F', flagFile)
)

// longValues returns value flags that have no shorthand.
func longValues(names ...string) []flagSpec {
	out := make([]flagSpec, 0, len(names))
	for _, n := range names {
		out = append(out, newFlag(n, 0, flagValue))
	}
	return out
}

func specs(groups ...[]flagSpec) []flagSpec {
	var out []flagSpec
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

var editValues = longValues("add-assignee", "remove-assignee", "add-label", "remove-label", "add-project", "remove-project", "milestone")

// ghWriteSpecs are the gh commands that publish a title, body or comment,
// with the flags that take a value.
var ghWriteSpecs = map[string][]flagSpec{
	"pr create":      {flagTitle, flagBody, flagBodyFile},
	"pr edit":        specs([]flagSpec{flagTitle, flagBody, flagBodyFile, newFlag("base", 'B', flagValue)}, editValues, longValues("add-reviewer", "remove-reviewer")),
	"pr comment":     {flagBody, flagBodyFile},
	"pr review":      {flagBody, flagBodyFile},
	"pr merge":       specs([]flagSpec{newFlag("subject", 't', flagText), flagBody, flagBodyFile}, longValues("match-head-commit", "author-email")),
	"pr close":       {flagComment},
	"pr reopen":      {flagComment},
	"issue create":   {flagTitle, flagBody, flagBodyFile},
	"issue edit":     specs([]flagSpec{flagTitle, flagBody, flagBodyFile}, editValues),
	"issue comment":  {flagBody, flagBodyFile},
	"issue close":    {flagComment, newFlag("reason", 'r', flagValue)},
	"issue reopen":   {flagComment},
	"release create": specs([]flagSpec{flagTitle, flagNotes, flagNotesFile}, longValues("target", "discussion-category", "notes-start-tag")),
	"release edit":   specs([]flagSpec{flagTitle, flagNotes, flagNotesFile}, longValues(wordTag, "target", "discussion-category")),
}

// ghReadVerbs are the pr, issue, release and other subcommands that publish
// nothing and change no file.
var ghReadVerbs = map[string]struct{}{
	"checks": {},
	"diff":   {},
	"list":   {},
	"status": {},
	"view":   {},
}

// ghOtherGroups are the gh commands that publish no title, body or comment
// this hook checks. Anything else may be an alias or an extension.
var ghOtherGroups = map[string]struct{}{
	"alias":       {},
	"attestation": {},
	"auth":        {},
	"browse":      {},
	"cache":       {},
	"co":          {},
	"codespace":   {},
	"completion":  {},
	"config":      {},
	"extension":   {},
	"gist":        {},
	"gpg-key":     {},
	"help":        {},
	"label":       {},
	"org":         {},
	"project":     {},
	"repo":        {},
	"ruleset":     {},
	"run":         {},
	"search":      {},
	"secret":      {},
	"ssh-key":     {},
	"status":      {},
	"variable":    {},
	"version":     {},
	"workflow":    {},
}

// ghPublishes reports whether a gh command line publishes.
func ghPublishes(words []string, dyn []bool) pubKind {
	p := parseFlags(words[1:], dyn[1:], []flagSpec{flagRepo})
	if len(p.pos) == 0 {
		return pubNone
	}
	group := p.pos[0]
	if p.posDyn[0] {
		return pubUnknown
	}
	switch group {
	case wordAPI:
		if apiWrites(words, dyn) {
			return pubGH
		}
		return pubNone
	case "pr", wordIssue, wordRelease:
		if len(p.pos) < 2 {
			return pubNone
		}
		if p.posDyn[1] {
			return pubUnknown
		}
		if _, ok := ghWriteSpecs[group+" "+p.pos[1]]; ok {
			return pubGH
		}
		return pubNone
	}
	if _, ok := ghOtherGroups[group]; ok {
		return pubNone
	}
	return pubUnknown
}

// ghInert reports whether a gh command changes nothing a later publication
// reads: a read, or a publication itself.
func ghInert(words []string, dyn []bool) bool {
	if anyTrue(dyn) {
		return false
	}
	if publishes(words, dyn) == pubGH {
		return true
	}
	p := parseFlags(words[1:], dyn[1:], []flagSpec{flagRepo})
	if len(p.pos) == 0 {
		return true
	}
	switch p.pos[0] {
	case wordAPI:
		return true // not a write: publishes said so
	case "pr", wordIssue, wordRelease, "repo", "run", "workflow", "auth", "search":
		if len(p.pos) < 2 {
			return true
		}
		_, ok := ghReadVerbs[p.pos[1]]
		return ok
	}
	return false
}

// apiSpecs are the gh api flags that take a value.
var apiSpecs = []flagSpec{
	newFlag("method", 'X', flagValue),
	newFlag("raw-field", 'f', flagText),
	newFlag("field", 'F', flagText),
	newFlag("input", 0, flagFile),
	newFlag("header", 'H', flagValue),
	newFlag("jq", 'q', flagValue),
	newFlag("template", 't', flagValue),
	newFlag("hostname", 0, flagValue),
	newFlag("preview", 'p', flagValue),
	newFlag("cache", 0, flagValue),
}

// apiWrites reports whether a gh api call may write: it has fields or an
// input, a method other than GET, or arguments the hook cannot read.
func apiWrites(words []string, dyn []bool) bool {
	if anyTrue(dyn) {
		return true
	}
	p := parseFlags(words[1:], dyn[1:], apiSpecs)
	for _, f := range p.flags {
		switch f.spec.long {
		case "method":
			if !strings.EqualFold(f.value, "GET") {
				return true
			}
		case "raw-field", "field", "input":
			return true
		}
	}
	return false
}

var prURL = regexp.MustCompile(`^https?://(?:www\.)?github\.com/([^/]+/[^/]+)/(?:pull|issues)/`)

// ghURLRepo returns the owner/name of a pull request or issue URL, or "".
func ghURLRepo(s string) string {
	m := prURL.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	return m[1]
}
