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
