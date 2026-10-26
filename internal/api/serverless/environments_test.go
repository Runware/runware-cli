package serverless

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/runware/runware-cli/internal/api/transport"
)

func TestEnvironmentCallsNeedAnAPIKey(t *testing.T) {
	c := NewClient("", "https://example.invalid", slog.Default())
	for _, call := range orgScopedCalls() {
		switch call.operation {
		case "CreateEnvironment", "UpdateEnvironment", "DeleteEnvironment", "GetEnvironmentDefaults", "PutEnvironmentDefaults":
		default:
			continue
		}
		t.Run(call.operation, func(t *testing.T) {
			if err := call.call(context.Background(), c); !errors.Is(err, transport.ErrNoAPIKey) {
				t.Fatalf("err = %v, want ErrNoAPIKey", err)
			}
		})
	}
}
