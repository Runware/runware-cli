package api

import (
	"encoding/json"
	"reflect"
	"slices"
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

func TestModelResultDecodesEveryField(t *testing.T) {
	row := `{
		"air": "civitai:1015427@1138416",
		"name": "Wan Motion LoRA",
		"category": "lora",
		"architecture": "wan_2_1_14b",
		"capabilities": ["io:text-to-video"],
		"source": "community",
		"heroImage": "https://im.runware.ai/image/hero.jpg",
		"private": true,
		"isFavorite": true,
		"provider": "civitai",
		"shortDescription": "Camera motion for Wan",
		"positiveTriggerWords": "orbit shot",
		"negativeTriggerWords": "static",
		"comment": "Trained on drone footage",
		"type": "base",
		"version": "v2",
		"tags": ["motion", "camera"],
		"addedUnixTimestamp": 1736000000,
		"updatedDateUnixTimestamp": 1736500000,
		"floatingPoint": "bf16",
		"uniqueIdentifier": "9f86d081884c7d659a2feaa0c55ad015",
		"defaultWidth": 832,
		"defaultHeight": 480,
		"defaultSteps": 30,
		"defaultCFG": 5,
		"defaultScheduler": "UniPC",
		"defaultStrength": 0.9,
		"defaultWeight": 1.2,
		"defaultFPS": 16,
		"defaultNumberOfFrames": 81,
		"defaultDuration": 5.5
	}`
	want := ModelResult{
		AIR:                      "civitai:1015427@1138416",
		Name:                     "Wan Motion LoRA",
		Category:                 "lora",
		Architecture:             "wan_2_1_14b",
		Capabilities:             []string{"io:text-to-video"},
		Source:                   "community",
		HeroImage:                "https://im.runware.ai/image/hero.jpg",
		Private:                  true,
		IsFavorite:               new(true),
		Provider:                 "civitai",
		ShortDescription:         "Camera motion for Wan",
		PositiveTriggerWords:     "orbit shot",
		NegativeTriggerWords:     "static",
		Comment:                  "Trained on drone footage",
		Type:                     "base",
		Version:                  "v2",
		Tags:                     ModelTags{"motion", "camera"},
		AddedUnixTimestamp:       1736000000,
		UpdatedDateUnixTimestamp: 1736500000,
		FloatingPoint:            "bf16",
		UniqueIdentifier:         "9f86d081884c7d659a2feaa0c55ad015",
		DefaultWidth:             832,
		DefaultHeight:            480,
		DefaultSteps:             30,
		DefaultCFG:               5,
		DefaultScheduler:         "UniPC",
		DefaultStrength:          0.9,
		DefaultWeight:            1.2,
		DefaultFPS:               16,
		DefaultNumberOfFrames:    81,
		DefaultDuration:          5.5,
	}

	var got ModelResult
	if err := json.Unmarshal([]byte(row), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("decoded row differs:\n got %+v\nwant %+v", got, want)
	}

	printed, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var sentFields, printedFields map[string]any
	if err := json.Unmarshal([]byte(row), &sentFields); err != nil {
		t.Fatalf("unmarshal row: %v", err)
	}
	if err := json.Unmarshal(printed, &printedFields); err != nil {
		t.Fatalf("unmarshal printed: %v", err)
	}
	if !reflect.DeepEqual(printedFields, sentFields) {
		t.Errorf("printed JSON differs from the row:\n got %s\nwant %s", printed, row)
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
	for _, key := range []string{"search", "capabilities", "category", "architecture", "visibility", "offset"} {
		if _, ok := got[key]; ok {
			t.Errorf("unexpected %q in the payload: %s", key, data)
		}
	}
}

func TestModelSearchRequestSendsCapabilities(t *testing.T) {
	data, err := json.Marshal(ModelSearchRequest{
		TaskType: taskTypeModelSearch,
		Capabilities: []string{
			"io:text-to-image",
			"op:inpaint",
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	sent, _ := got["capabilities"].([]any)
	if want := []any{"io:text-to-image", "op:inpaint"}; !slices.Equal(sent, want) {
		t.Errorf("capabilities = %v, want %v in %s", sent, want, data)
	}
}
