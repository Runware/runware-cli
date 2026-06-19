package model

import (
	"testing"

	"github.com/runware/runware-cli/internal/api"
)

func TestModelPricingRows(t *testing.T) {
	r := modelPricing{p: &api.ModelPricing{
		PricingExamples: []api.PricingExample{
			{Configuration: "Input", Price: "$2"},
			{Configuration: "Output", Price: "$12"},
		},
	}}
	rows := r.Rows()
	if len(rows) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(rows))
	}
	if rows[0][0] != "Input" || rows[0][1] != "$2" {
		t.Errorf("unexpected first row: %v", rows[0])
	}
}

func TestModelExamplesRows_EmptyTitleDashed(t *testing.T) {
	r := modelExamples{examples: []api.ModelExample{
		{ID: "ex-1", Title: "", Capability: "io:text-to-image"},
	}}
	rows := r.Rows()
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0][0] != "—" {
		t.Errorf("empty title should render as a dash, got: %v", rows[0][0])
	}
	if rows[0][2] != "ex-1" {
		t.Errorf("unexpected id column: %v", rows[0][2])
	}
}
