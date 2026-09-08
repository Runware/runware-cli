package serverless

import (
	"errors"
	"strings"
	"testing"
)

func collectSSE(t *testing.T, stream string) []sseEvent {
	t.Helper()
	var got []sseEvent
	err := readSSEEvents(strings.NewReader(stream), func(ev sseEvent) error {
		got = append(got, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("readSSEEvents: %v", err)
	}
	return got
}

func TestReadSSEEvents_DataAndNamedEvents(t *testing.T) {
	got := collectSSE(t, "data: {\"time\":1750000000,\"body\":\"ready\"}\n\nevent: end\ndata: \n\n")
	if len(got) != 2 {
		t.Fatalf("events = %#v", got)
	}
	if got[0].Event != "" || got[0].Data != `{"time":1750000000,"body":"ready"}` {
		t.Errorf("first = %#v", got[0])
	}
	if got[1].Event != sseEventEnd || got[1].Data != "" {
		t.Errorf("second = %#v", got[1])
	}
}

func TestReadSSEEvents_SkipsKeepaliveComments(t *testing.T) {
	got := collectSSE(t, ": keepalive\n\n: keepalive\n\ndata: a\n\n")
	if len(got) != 1 || got[0].Data != "a" {
		t.Fatalf("events = %#v", got)
	}
}

func TestReadSSEEvents_JoinsMultiLineDataAndTrimsCR(t *testing.T) {
	got := collectSSE(t, "event: error\r\ndata: first\r\ndata: second\r\n\r\n")
	if len(got) != 1 || got[0].Event != sseEventError || got[0].Data != "first\nsecond" {
		t.Fatalf("events = %#v", got)
	}
}

func TestReadSSEEvents_DispatchesTrailingEventAtEOF(t *testing.T) {
	got := collectSSE(t, "data: tail")
	if len(got) != 1 || got[0].Data != "tail" {
		t.Fatalf("events = %#v", got)
	}
}

func TestReadSSEEvents_StopsOnHandlerError(t *testing.T) {
	sentinel := errors.New("stop")
	calls := 0
	err := readSSEEvents(strings.NewReader("data: a\n\ndata: b\n\n"), func(sseEvent) error {
		calls++
		return sentinel
	})
	if !errors.Is(err, sentinel) || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestReadSSEEvents_RejectsAnEventLargerThanTheCapAcrossLines(t *testing.T) {
	line := "data: " + strings.Repeat("x", 1<<20) + "\n"
	stream := strings.Repeat(line, maxSSEFrameBytes>>20+1) + "\n"
	err := readSSEEvents(strings.NewReader(stream), func(sseEvent) error {
		t.Fatal("an oversized event must not reach the handler")
		return nil
	})
	if !errors.Is(err, ErrSSEEventTooLarge) {
		t.Fatalf("err = %v", err)
	}
}
