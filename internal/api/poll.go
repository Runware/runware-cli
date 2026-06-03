package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/runware/runware-cli/internal/api/transport"
)

// PollResults polls for async task results by repeatedly calling GetResponse via the transport
// until the parse function yields at least one result, the context is cancelled, or a fatal
// error (auth or API error) is returned.
func PollResults[T any](ctx context.Context, t transport.Transport, taskID uuid.UUID, interval time.Duration, logger *slog.Logger, parse func(json.RawMessage) (T, bool)) ([]T, error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var results []T
	for {
		tasks := []any{
			&GetResponseRequest{
				TaskType: taskTypeGetResponse,
				TaskUUID: taskID,
			},
		}

		data, err := t.Send(ctx, tasks)
		if err != nil {
			var re *transport.RunwareError
			if errors.As(err, &re) || transport.IsAuthError(err) {
				return nil, err
			}
			if logger != nil && logger.Enabled(ctx, slog.LevelDebug) {
				logger.Debug("poll error", "err", err)
			}
		} else {
			for _, raw := range data {
				if r, ok := parse(raw); ok {
					results = append(results, r)
				}
			}
			if len(results) > 0 {
				return results, nil
			}
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
