package transport

import (
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
