package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
)

// PollResults polls for generic results from a task.
func PollResults[T any](ctx context.Context, client Client, taskID uuid.UUID, interval time.Duration, verbose bool, parse func(json.RawMessage) (T, bool)) ([]T, error) {
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
			if verbose {
				fmt.Fprintf(os.Stderr, "Poll: %s\n", err) //nolint:errcheck,gosec
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
