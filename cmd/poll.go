package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/runware/runware-cli/internal/api"
)

func pollResults[T any](ctx context.Context, client api.Client, taskUUID string, interval time.Duration, parse func(json.RawMessage) (T, bool)) ([]T, error) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var results []T
	for {
		rawData, err := client.GetResponse(ctx, taskUUID)
		if err != nil {
			var apiErr api.APIError
			if api.IsAuthError(err) || errors.As(err, &apiErr) {
				return nil, err
			}
			if flagVerbose {
				_, _ = fmt.Fprintf(os.Stderr, "Poll: %s\n", err)
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
			return nil, nil
		case <-ticker.C:
		}
	}
}
