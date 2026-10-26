package serverless

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/runware/runware-cli/internal/api/transport"
)

func TestOrganizationCallsNeedAnAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	for _, call := range orgScopedCalls() {
		t.Run(call.operation, func(t *testing.T) {
			if err := call.call(context.Background(), c); !errors.Is(err, transport.ErrNoAPIKey) {
				t.Fatalf("err = %v, want ErrNoAPIKey", err)
			}
		})
	}
}
