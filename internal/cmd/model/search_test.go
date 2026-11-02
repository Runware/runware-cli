package model

import (
	"strings"
	"testing"

	"github.com/runware/runware-cli/internal/api"
)

func TestValidateSearch(t *testing.T) {
	tests := []struct {
		name    string
		search  string
		wantErr bool
	}{
		{name: "empty", search: "", wantErr: false},
		{name: "one character", search: "f", wantErr: true},
		{name: "two characters", search: "fl", wantErr: false},
		{name: "48 characters", search: strings.Repeat("a", 48), wantErr: false},
		{name: "49 characters", search: strings.Repeat("a", 49), wantErr: true},
		{name: "one multibyte character", search: "é", wantErr: true},
		{name: "48 multibyte characters", search: strings.Repeat("é", 48), wantErr: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSearch(tt.search)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateSearch(%q) error = %v, wantErr %v", tt.search, err, tt.wantErr)
			}
		})
	}
}

func TestModelSearchRowsIncludeSource(t *testing.T) {
	r := modelSearchResults{models: []api.ModelResult{
		{
			Name:     "PhotoRealistic XL",
			AIR:      "civitai:249421@281440",
			Source:   "community",
			Category: "checkpoint",
		},
	}}
	headers := r.Headers()
	rows := r.Rows()
	if headers[2] != colSource {
		t.Fatalf("expected the third column to be %q, got %q", colSource, headers[2])
	}
	if rows[0][2] != "community" {
		t.Errorf("expected source community, got %v", rows[0][2])
	}
}
