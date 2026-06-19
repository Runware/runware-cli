package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const pricingBody = `{
	"model": "google-gemini-3-1-pro",
	"air": "google:gemini@3.1-pro",
	"name": "Gemini 3.1 Pro",
	"status": "live",
	"pricingOverview": "Token-based pricing",
	"pricingExamples": [
		{"configuration": "Input tokens (<200k)", "price": "$2 / 1M"},
		{"configuration": "Output tokens", "price": "$12 / 1M"}
	],
	"category": ["text"]
}`

const examplesBody = `[
	{
		"id": "ex-1",
		"title": "Incident Brief",
		"model": "google-gemini-3-1-pro",
		"capability": "io:text-to-image",
		"request": {"positivePrompt": "a cat"},
		"response": {"imageURL": "https://im.runware.ai/x.jpg"}
	}
]`

func contentServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, srv.URL + "/"
}

func TestFetchModelPricing_200(t *testing.T) {
	srv, base := contentServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "google:gemini@3.1-pro/pricing") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(pricingBody))
	})

	pricing, err := fetchModelPricing(context.Background(), "google:gemini@3.1-pro", srv.Client(), base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pricing.Name != "Gemini 3.1 Pro" {
		t.Errorf("unexpected Name: %s", pricing.Name)
	}
	if len(pricing.PricingExamples) != 2 {
		t.Fatalf("expected 2 pricing examples, got %d", len(pricing.PricingExamples))
	}
	if pricing.PricingExamples[0].Price != "$2 / 1M" {
		t.Errorf("unexpected price: %s", pricing.PricingExamples[0].Price)
	}
}

func TestFetchModelPricing_404(t *testing.T) {
	srv, base := contentServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})

	_, err := fetchModelPricing(context.Background(), "no:such@model", srv.Client(), base)
	if err == nil {
		t.Fatal("expected an error for a 404 response")
	}
	if !strings.Contains(err.Error(), "no:such@model") {
		t.Errorf("error should name the identifier; got: %v", err)
	}
}

func TestFetchModelExamples_200(t *testing.T) {
	srv, base := contentServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "google:gemini@3.1-pro/examples") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(examplesBody))
	})

	examples, err := fetchModelExamples(context.Background(), "google:gemini@3.1-pro", srv.Client(), base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(examples) != 1 {
		t.Fatalf("expected 1 example, got %d", len(examples))
	}
	if examples[0].Capability != "io:text-to-image" {
		t.Errorf("unexpected capability: %s", examples[0].Capability)
	}
	if examples[0].Request["positivePrompt"] != "a cat" {
		t.Errorf("request not parsed: %v", examples[0].Request)
	}
}
