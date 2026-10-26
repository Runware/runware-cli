package serverless

import (
	"errors"
	"fmt"
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
	detail := "available credit cannot cover the requested capacity"
	p := &gen.ProblemDetails{
		Title:  "Payment Required",
		Status: 402,
		Detail: &detail,
		Shortfall: &gen.MoneyAmount{
			Amount:   "12.50",
			Currency: gen.USD,
		},
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
	if !strings.Contains(re.Message, "shortfall: 12.50 USD") {
		t.Errorf("missing shortfall: %q", re.Message)
	}
}

func TestProblemToError_EnvironmentPausedIsNotRetryable(t *testing.T) {
	const pausedType = "https://docs.runware.ai/serverless/errors#environment-paused"
	const detail = "environment staging is paused: its spend this month reached its monthly budget"
	// Invoke answers the pause with 429; deploy, resume and scale with 409.
	for _, status := range []int{http.StatusTooManyRequests, http.StatusConflict} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			body := fmt.Sprintf(`{"type":%q,"title":%q,"status":%d,"detail":%q}`, pausedType, http.StatusText(status), status, detail)
			var re *transport.RunwareError
			if !errors.As(problemFromBody([]byte(body), status), &re) {
				t.Fatalf("expected *transport.RunwareError")
			}
			if re.RawCode != "environmentPaused" || re.Code != transport.CodeQuota || re.Retryable {
				t.Errorf("raw %q code %q retryable %v, want environmentPaused quota false", re.RawCode, re.Code, re.Retryable)
			}
			if re.ProblemType != pausedType {
				t.Errorf("problem type %q, want %q", re.ProblemType, pausedType)
			}
			if re.Message != detail {
				t.Errorf("message %q, want the detail", re.Message)
			}
		})
	}
}

func TestProblemToError_TooManyRequestsIsARetryableRateLimit(t *testing.T) {
	err := problemFromBody([]byte(`{"type":"about:blank","title":"Too Many Requests","status":429}`), http.StatusTooManyRequests)
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.RawCode != "rateLimitExceeded" || re.Code != transport.CodeRateLimit || !re.Retryable {
		t.Errorf("raw %q code %q retryable %v, want rateLimitExceeded rateLimit true", re.RawCode, re.Code, re.Retryable)
	}
	if re.ProblemType != "" {
		t.Errorf("problem type %q, want none for about:blank", re.ProblemType)
	}
}

func TestProblemToError_KeepsTheProblemTypeOfAConflict(t *testing.T) {
	const capType = "https://docs.runware.ai/serverless/errors#environment-cap-reached"
	err := problemFromBody([]byte(`{"type":"`+capType+`","title":"Conflict","status":409,"detail":"cap is 1"}`), http.StatusConflict)
	var re *transport.RunwareError
	if !errors.As(err, &re) {
		t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
	}
	if re.ProblemType != capType {
		t.Errorf("problem type %q, want %q", re.ProblemType, capType)
	}
	if re.RawCode != "conflict" || re.Code != transport.CodeValidation {
		t.Errorf("raw %q code %q, want conflict validation", re.RawCode, re.Code)
	}
}
