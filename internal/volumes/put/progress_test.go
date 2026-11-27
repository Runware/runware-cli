package put

import (
	"io"
	"strings"
	"testing"
	"time"
)

func TestFormatBytes(t *testing.T) {
	t.Parallel()
	for n, want := range map[int64]string{
		0:               "0 B",
		1023:            "1023 B",
		1024:            "1.0 KiB",
		1536:            "1.5 KiB",
		5 << 20:         "5.0 MiB",
		250 << 30:       "250.0 GiB",
		3 << 40:         "3.0 TiB",
		1<<20 - 1:       "1024.0 KiB",
		10270159184:     "9.6 GiB",
		1<<62 + 1<<61:   "6.0 EiB",
		(1 << 50) * 2.5: "2.5 PiB",
	} {
		if got := formatBytes(n); got != want {
			t.Errorf("formatBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestProgressLine(t *testing.T) {
	t.Parallel()
	start := time.Unix(1000, 0)
	p := &progress{
		label:    "uploading",
		started:  start,
		filesTot: 10,
		bytesTot: 100 << 20,
	}
	p.files.Store(3)
	p.bytes.Store(25 << 20)

	if got, want := p.line(start.Add(5*time.Second)), "uploading: 3/10 files, 25.0 MiB of 100.0 MiB (25%), 5.0 MiB/s, 15s left"; got != want {
		t.Errorf("line = %q, want %q", got, want)
	}
	if got := p.line(start.Add(500 * time.Millisecond)); strings.Contains(got, "/s") || strings.Contains(got, "left") {
		t.Errorf("line %q reports a rate before a second has passed", got)
	}
	p.bytes.Store(100 << 20)
	if got := p.line(start.Add(20 * time.Second)); strings.Contains(got, "left") || !strings.Contains(got, "(100%)") {
		t.Errorf("line %q for a finished phase", got)
	}
}

func TestNilProgressIsSilent(t *testing.T) {
	t.Parallel()
	var p *progress
	p.begin("hashing", 1, 1)
	p.addBytes(1)
	p.fileDone()
	p.end()
	if got := newProgress(nil, true, time.Second, time.Now, nil); got != nil {
		t.Error("a progress without a writer is not nil")
	}
}

// TestProgressIntervals: a line every few seconds off a terminal, and a rewrite each second on one.
func TestProgressIntervals(t *testing.T) {
	t.Parallel()
	if p := newProgress(io.Discard, false, 0, time.Now, nil); p.every < time.Second || p.every > 10*time.Second {
		t.Errorf("the default interval off a terminal is %v, want a few seconds", p.every)
	}
	if p := newProgress(io.Discard, true, 0, time.Now, nil); p.every != time.Second {
		t.Errorf("the default interval on a terminal is %v, want one second", p.every)
	}
	if p := newProgress(io.Discard, true, 7*time.Second, time.Now, nil); p.every != time.Second {
		t.Errorf("a terminal given 7s rewrites every %v, want one second", p.every)
	}
	if p := newProgress(io.Discard, false, 7*time.Second, time.Now, nil); p.every != 7*time.Second {
		t.Errorf("a writer given 7s prints every %v, want 7s", p.every)
	}
}

// waitFor polls until cond holds, for the one goroutine that prints on a tick.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for range 400 {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestProgressTerminalRewritesOneLine(t *testing.T) {
	t.Parallel()
	var buf lockedBuffer
	tick := make(chan time.Time)
	now := time.Unix(1000, 0)
	p := newProgress(&buf, true, time.Minute, func() time.Time { return now }, tick)

	p.begin("hashing", 2, 2048)
	p.addBytes(1024)
	p.fileDone()
	tick <- now
	waitFor(t, "the first line", func() bool { return strings.Contains(buf.String(), "hashing: 1/2 files, 1.0 KiB of 2.0 KiB (50%)") })
	p.addBytes(1024)
	p.fileDone()
	p.end()

	out := buf.String()
	if strings.Count(out, "\r") != 2 || strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, "\n") {
		t.Errorf("terminal output = %q, want two carriage-return lines and one final newline", out)
	}
	if !strings.Contains(out, "hashing: 2/2 files, 2.0 KiB of 2.0 KiB (100%)") {
		t.Errorf("terminal output lacks the last state: %q", out)
	}
}

func TestProgressPrintsLinesWithoutATerminal(t *testing.T) {
	t.Parallel()
	var buf lockedBuffer
	tick := make(chan time.Time)
	now := time.Unix(1000, 0)
	p := newProgress(&buf, false, time.Minute, func() time.Time { return now }, tick)

	p.begin("uploading", 1, 2048)
	p.addBytes(1024)
	tick <- now
	waitFor(t, "the periodic line", func() bool { return strings.Contains(buf.String(), "uploading: 0/1 files, 1.0 KiB of 2.0 KiB (50%)\n") })
	p.addBytes(1024)
	p.fileDone()
	p.end()

	out := buf.String()
	if strings.Contains(out, "\r") || strings.Count(out, "\n") != 2 {
		t.Errorf("output = %q, want two plain lines", out)
	}
	if !strings.HasSuffix(out, "uploading: 1/1 files, 2.0 KiB of 2.0 KiB (100%)\n") {
		t.Errorf("output lacks the last state: %q", out)
	}
}

func TestProgressPhasesEndEachOther(t *testing.T) {
	t.Parallel()
	var buf lockedBuffer
	p := newProgress(&buf, false, time.Minute, time.Now, nil)

	p.begin("hashing", 1, 10)
	p.addBytes(10)
	p.fileDone()
	p.begin("uploading", 1, 20)
	p.end()

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "hashing: 1/1 files") || !strings.HasPrefix(lines[1], "uploading: 0/1 files") {
		t.Errorf("lines = %q, want a last line for each phase", lines)
	}
}
