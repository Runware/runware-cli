package api

import (
	"encoding/json"
	"testing"
)

func TestModelResultDecodesSearchRow(t *testing.T) {
	row := `{
		"name": "PhotoRealistic XL",
		"nameContent": "PhotoRealistic XL",
		"air": "civitai:249421@281440",
		"tags": ["character", "photorealistic"],
		"heroImage": null,
		"category": "checkpoint",
		"private": false,
		"version": "PhotoRealistic D XL",
		"architecture": null,
		"capabilities": ["form:checkpoint", "io:text-to-image"],
		"downloadCount": 12,
		"addedUnixTimestamp": 1705363501,
		"source": "community",
		"type": "base",
		"defaultWidth": null,
		"defaultSteps": 20,
		"defaultCFG": 7.5
	}`

	var m ModelResult
	if err := json.Unmarshal([]byte(row), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if m.Source != "community" || m.Type != "base" || m.Version != "PhotoRealistic D XL" {
		t.Errorf("unexpected identity fields: %+v", m)
	}
	if m.HeroImage != "" || m.Architecture != "" || m.DefaultWidth != 0 {
		t.Errorf("null fields should decode to zero values: %+v", m)
	}
	if len(m.Capabilities) != 2 || len(m.Tags) != 2 {
		t.Errorf("unexpected list fields: capabilities=%v tags=%v", m.Capabilities, m.Tags)
	}
	if m.AddedUnixTimestamp != 1705363501 || m.DefaultSteps != 20 || m.DefaultCFG != 7.5 {
		t.Errorf("unexpected numeric fields: %+v", m)
	}
}

func TestModelResultKeepsIsFavoriteAsSent(t *testing.T) {
	for _, row := range []string{
		`{"air": "civitai:249421@281440"}`,
		`{"air": "civitai:249421@281440", "isFavorite": false}`,
		`{"air": "civitai:249421@281440", "isFavorite": true}`,
	} {
		var m ModelResult
		if err := json.Unmarshal([]byte(row), &m); err != nil {
			t.Fatalf("unmarshal %s: %v", row, err)
		}
		data, err := json.Marshal(m)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var sent, printed map[string]any
		if err := json.Unmarshal([]byte(row), &sent); err != nil {
			t.Fatalf("unmarshal %s: %v", row, err)
		}
		if err := json.Unmarshal(data, &printed); err != nil {
			t.Fatalf("unmarshal %s: %v", data, err)
		}
		wantValue, wantPresent := sent["isFavorite"]
		gotValue, gotPresent := printed["isFavorite"]
		if wantPresent != gotPresent || wantValue != gotValue {
			t.Errorf("isFavorite: sent %s, printed %s", row, data)
		}
	}
}

func TestModelSearchRequestOmitsUnsetFilters(t *testing.T) {
	data, err := json.Marshal(ModelSearchRequest{
		TaskType: taskTypeModelSearch,
		Source:   "official",
		Sort:     "name",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["source"] != "official" || got["sort"] != "name" {
		t.Errorf("expected source and sort in the payload, got %s", data)
	}
	for _, key := range []string{"search", "capabilities", "category", "tags", "type", "conditioning"} {
		if _, ok := got[key]; ok {
			t.Errorf("unexpected %q in the payload: %s", key, data)
		}
	}
}
