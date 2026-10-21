package transport

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"testing"
)

func TestValidTransport(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"ws", true},
		{"http", true},
		{"WS", true},
		{"HTTP", true},
		{"wss", false},
		{"https", false},
		{"", false},
		{"bogus", false},
	}

	for _, tt := range tests {
		if got := ValidTransport(tt.in); got != tt.want {
			t.Errorf("ValidTransport(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestValidTransports(t *testing.T) {
	want := []string{
		SchemeWS,
		SchemeHTTP,
	}
	if got := ValidTransports(); !slices.Equal(got, want) {
		t.Errorf("ValidTransports() = %v, want %v", got, want)
	}
}

func TestDialContext_RejectsUnusableBaseURL(t *testing.T) {
	tests := []struct {
		name    string
		scheme  string
		baseURL string
	}{
		{"http with misspelled scheme", SchemeHTTP, "htps://api.runware.ai/v1"},
		{"http with websocket url", SchemeHTTP, "wss://ws-api.runware.ai/v1"},
		{"http without host", SchemeHTTP, "https:///v1"},
		{"http empty", SchemeHTTP, ""},
		{"websocket with http url", SchemeWS, "https://api.runware.ai/v1"},
		{"websocket unparsable", SchemeWS, "wss://ws api.runware.ai/v1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := DialContext(context.Background(), tt.scheme, "key", tt.baseURL, slog.Default())

			if !errors.Is(err, ErrInvalidBaseURL) {
				t.Fatalf("DialContext(%q, %q) error = %v, want ErrInvalidBaseURL", tt.scheme, tt.baseURL, err)
			}
		})
	}
}
