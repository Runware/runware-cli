package put

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// fail keeps interrupted uploads and exhausted transient retries for resume.
func (r *runner) fail(ctx context.Context, err error) error {
	cause := context.Cause(ctx)
	if cause != nil && (errors.Is(err, ctx.Err()) || errors.Is(err, cause)) {
		err = cause
	}
	if r.now().Before(r.expiresAt) &&
		(errors.Is(err, errUploadRetriesExhausted) || (cause != nil && errors.Is(err, cause))) {
		return r.suspend(err)
	}
	return r.abort(ctx, err)
}

// suspend leaves the session open and its resume state on disk, so running put again with the same paths resumes it.
func (r *runner) suspend(cause error) error {
	if r.journal != nil {
		if err := r.journal.close(); err != nil {
			r.warn("%v", err)
		}
	}
	if r.completionAttempted {
		return fmt.Errorf("%w (publication may have completed: run put again with the same paths before %s to recover the result)",
			cause, r.expiresAt.Format(time.RFC3339))
	}
	return fmt.Errorf("%w (the upload session stays open until %s: run put again with the same paths to resume it)",
		cause, r.expiresAt.Format(time.RFC3339))
}
