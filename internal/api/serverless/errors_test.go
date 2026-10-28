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

// problemTypeCase is a problem body, its status and the error it maps to.
type problemTypeCase struct {
	name            string
	status          int
	body            string
	wantRawCode     string
	wantCode        transport.ErrorCode
	wantRetryable   bool
	wantProblemType string
	// wantMessage is checked only when set.
	wantMessage string
}

func TestProblemToError_ProblemTypes(t *testing.T) {
	const pausedType = "https://docs.runware.ai/serverless/errors#environment-paused"
	const capType = "https://docs.runware.ai/serverless/errors#environment-cap-reached"
	const detail = "environment staging is paused: its spend this month reached its monthly budget"
	paused := func(status int) string {
		return fmt.Sprintf(`{"type":%q,"title":%q,"status":%d,"detail":%q}`, pausedType, http.StatusText(status), status, detail)
	}
	cases := []problemTypeCase{
		{
			name:            "paused invoke answers 429",
			status:          http.StatusTooManyRequests,
			body:            paused(http.StatusTooManyRequests),
			wantRawCode:     "environmentPaused",
			wantCode:        transport.CodeQuota,
			wantProblemType: pausedType,
			wantMessage:     detail,
		},
		{
			name:            "paused deploy, resume and scale answer 409",
			status:          http.StatusConflict,
			body:            paused(http.StatusConflict),
			wantRawCode:     "environmentPaused",
			wantCode:        transport.CodeQuota,
			wantProblemType: pausedType,
			wantMessage:     detail,
		},
		{
			name:          "about:blank 429 is a retryable rate limit",
			status:        http.StatusTooManyRequests,
			body:          `{"type":"about:blank","title":"Too Many Requests","status":429}`,
			wantRawCode:   "rateLimitExceeded",
			wantCode:      transport.CodeRateLimit,
			wantRetryable: true,
		},
		{
			name:            "conflict keeps its problem type",
			status:          http.StatusConflict,
			body:            `{"type":"` + capType + `","title":"Conflict","status":409,"detail":"cap is 1"}`,
			wantRawCode:     "conflict",
			wantCode:        transport.CodeValidation,
			wantProblemType: capType,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := problemFromBody([]byte(tc.body), tc.status)
			var re *transport.RunwareError
			if !errors.As(err, &re) {
				t.Fatalf("expected *transport.RunwareError, got %T: %v", err, err)
			}
			if re.RawCode != tc.wantRawCode || re.Code != tc.wantCode || re.Retryable != tc.wantRetryable {
				t.Errorf("raw %q code %q retryable %v, want %q %q %v", re.RawCode, re.Code, re.Retryable, tc.wantRawCode, tc.wantCode, tc.wantRetryable)
			}
			if re.ProblemType != tc.wantProblemType {
				t.Errorf("problem type %q, want %q", re.ProblemType, tc.wantProblemType)
			}
			if tc.wantMessage != "" && re.Message != tc.wantMessage {
				t.Errorf("message %q, want %q", re.Message, tc.wantMessage)
			}
		})
	}
}
