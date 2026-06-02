package api

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
)

// PollResults polls for generic results from a task.
func PollResults[T any](ctx context.Context, client Client, taskID uuid.UUID, interval time.Duration, logger *slog.Logger, parse func(json.RawMessage) (T, bool)) ([]T, error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var results []T
	for {
		rawData, err := client.GetResponse(ctx, taskID)
		if err != nil {
			var apiErr APIError
			if IsAuthError(err) || errors.As(err, &apiErr) {
				return nil, err
			}
			if logger != nil && logger.Enabled(ctx, slog.LevelDebug) {
				logger.Debug("poll error", "err", err)
			}
		} else {
			for _, raw := range rawData {
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
