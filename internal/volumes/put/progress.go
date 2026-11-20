package put

import (
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

const defaultProgressEvery = 5 * time.Second

// progress reports files and bytes done: a rewritten line on a terminal, a line per interval otherwise.
type progress struct {
	w     io.Writer
	tty   bool
	every time.Duration
	now   func() time.Time
	tick  <-chan time.Time

	files atomic.Int64
	bytes atomic.Int64

	mu       sync.Mutex
	label    string
	started  time.Time
	filesTot int
	bytesTot int64
	width    int
	stop     chan struct{}
	done     chan struct{}
}

func newProgress(w io.Writer, tty bool, every time.Duration, now func() time.Time, tick <-chan time.Time) *progress {
	if w == nil {
		return nil
	}
	if every <= 0 {
		every = defaultProgressEvery
	}
	if tty {
		every = min(every, time.Second)
	}
	return &progress{
		w:     w,
		tty:   tty,
		every: every,
		now:   now,
		tick:  tick,
	}
}

// begin starts a phase with its totals, ending the one running.
func (p *progress) begin(label string, files int, bytes int64) {
	if p == nil {
		return
	}
	p.end()
	p.mu.Lock()
	p.label, p.filesTot, p.bytesTot = label, files, bytes
	p.started = p.now()
	p.width = 0
	p.stop = make(chan struct{})
	p.done = make(chan struct{})
	stop, done := p.stop, p.done
	p.mu.Unlock()
	p.files.Store(0)
	p.bytes.Store(0)
	go p.loop(stop, done)
}

func (p *progress) fileDone() {
	if p != nil {
		p.files.Add(1)
	}
}

func (p *progress) addBytes(n int64) {
	if p != nil {
		p.bytes.Add(n)
	}
}

// end stops the running phase and prints its last line.
func (p *progress) end() {
	if p == nil {
		return
	}
	p.mu.Lock()
	stop, done := p.stop, p.done
	p.stop, p.done = nil, nil
	p.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	<-done
	p.print(true)
}

func (p *progress) loop(stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	tick := p.tick
	if tick == nil {
		t := time.NewTicker(p.every)
		defer t.Stop()
		tick = t.C
	}
	for {
		select {
		case <-stop:
			return
		case <-tick:
			p.print(false)
		}
	}
}

func (p *progress) print(final bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	line := p.line(p.now())
	switch {
	case !p.tty:
		_, _ = fmt.Fprintln(p.w, line)
	case final:
		_, _ = fmt.Fprintf(p.w, "\r%-*s\n", p.width, line)
	default:
		p.width = max(p.width, len(line))
		_, _ = fmt.Fprintf(p.w, "\r%-*s", p.width, line)
	}
}

// line is the progress text at now; the caller holds mu.
func (p *progress) line(now time.Time) string {
	files, done := p.files.Load(), p.bytes.Load()
	s := fmt.Sprintf("%s: %d/%d files, %s of %s", p.label, files, p.filesTot, formatBytes(done), formatBytes(p.bytesTot))
	if p.bytesTot > 0 {
		s += fmt.Sprintf(" (%d%%)", done*100/p.bytesTot)
	}
	if elapsed := now.Sub(p.started); elapsed >= time.Second && done > 0 {
		rate := float64(done) / elapsed.Seconds()
		s += fmt.Sprintf(", %s/s", formatBytes(int64(rate)))
		if left := p.bytesTot - done; left > 0 {
			s += fmt.Sprintf(", %s left", time.Duration(float64(left)/rate*float64(time.Second)).Round(time.Second))
		}
	}
	return s
}

// formatBytes writes n with a binary unit.
func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
