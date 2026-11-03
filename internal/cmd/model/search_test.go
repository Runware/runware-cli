package model

import (
	"slices"
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
	for _, wide := range []bool{false, true} {
		r := modelSearchResults{
			models: []api.ModelResult{
				{
					Name:     "PhotoRealistic XL",
					AIR:      "civitai:249421@281440",
					Source:   "community",
					Category: "checkpoint",
				},
			},
			wide: wide,
		}
		headers := r.Headers()
		i := slices.Index(headers, colSource)
		if i < 0 {
			t.Fatalf("wide=%v: expected a %q column, got %v", wide, colSource, headers)
		}
		if got := r.Rows()[0][i]; got != "community" {
			t.Errorf("wide=%v: expected source community, got %v", wide, got)
		}
	}
}
