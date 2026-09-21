package serverless

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/runware/runware-cli/internal/api/serverless/gen"
	"github.com/runware/runware-cli/internal/api/transport"
)

func TestProblemToError_IncludesValidationErrors(t *testing.T) {
	detail := "env var name collides with a plain environment variable"
	pointer := "/envVarName"
	p := &gen.ProblemDetails{
		Title:  "Unprocessable Entity",
		Status: 422,
		Detail: &detail,
		Errors: &[]gen.ProblemError{{
			Detail:  "already exists as an environment variable",
			Pointer: &pointer,
		}},
	}

	err := problemToError(p, http.StatusUnprocessableEntity)
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.Code != transport.CodeValidation {
		t.Errorf("expected CodeValidation, got %v", re.Code)
	}
	if !strings.Contains(re.Message, detail) {
		t.Errorf("missing problem detail: %q", re.Message)
	}
	if !strings.Contains(re.Message, "/envVarName: already exists as an environment variable") {
		t.Errorf("missing field error: %q", re.Message)
	}
}

func TestProblemToError_PaymentRequiredIncludesShortfall(t *testing.T) {
	detail := "Organization credit cannot cover the requested capacity"
	shortfall := "12.50"
	p := &gen.ProblemDetails{
		Title:     "Payment Required",
		Status:    402,
		Detail:    &detail,
		Shortfall: &shortfall,
	}

	err := problemToError(p, http.StatusPaymentRequired)
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.Code != transport.CodeQuota {
		t.Errorf("expected CodeQuota, got %v", re.Code)
	}
	if re.StatusCode != http.StatusPaymentRequired {
		t.Errorf("status %d, want 402", re.StatusCode)
	}
	if !strings.Contains(re.Message, detail) {
		t.Errorf("missing problem detail: %q", re.Message)
	}
	if !strings.Contains(re.Message, "shortfall: 12.50") {
		t.Errorf("missing shortfall: %q", re.Message)
	}
}
