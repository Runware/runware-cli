package serverless

import (
	"bufio"
	"fmt"
	"io"
	"strings"
)

// maxSSEFrameBytes bounds one event; the server caps its frames at the same size.
const maxSSEFrameBytes = 8 << 20

// The named events that end a log stream. Every other event carries an entry.
const (
	sseEventEnd   = "end"
	sseEventError = "error"
)

// sseEvent is one Server-Sent Event: the event name (empty for the default
// message event) and the data lines joined with newlines.
type sseEvent struct {
	Event string
	Data  string
}

// readSSEEvents parses a text/event-stream body and hands every event to
// handle, in order, until the body ends, a read fails or handle returns an
// error. Comment lines (the server's keepalives) are dropped.
func readSSEEvents(r io.Reader, handle func(sseEvent) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), maxSSEFrameBytes)

	var (
		ev      sseEvent
		data    []string
		pending bool
	)
	dispatch := func() error {
		if !pending {
			return nil
		}
		ev.Data = strings.Join(data, "\n")
		err := handle(ev)
		ev, data, pending = sseEvent{}, nil, false
		return err
	}

	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		switch {
		case line == "":
			if err := dispatch(); err != nil {
				return err
			}
		case strings.HasPrefix(line, ":"):
			// Comment: keepalive.
		default:
			field, value, _ := strings.Cut(line, ":")
			value = strings.TrimPrefix(value, " ")
			switch field {
			case "event":
				ev.Event = value
				pending = true
			case "data":
				data = append(data, value)
				pending = true
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read log stream: %w", err)
	}
	return dispatch()
}
