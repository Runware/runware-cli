package put

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// fail keeps interrupted uploads and exhausted transient retries for resume.
func (r *runner) fail(ctx context.Context, err error) error {
	if errors.Is(err, ErrSourceChanged) {
		return r.abort(ctx, err)
	}
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
	deadline := r.expiresAt.Format(time.RFC3339)
	hint := fmt.Sprintf("the upload session stays open until %s: run put again with the same paths to resume it", deadline)
	if r.completionAttempted {
		hint = fmt.Sprintf("publication may have completed: run put again with the same paths before %s to recover the result", deadline)
	}
	return &suspendedUploadError{
		cause: cause,
		hint:  hint,
	}
}

type suspendedUploadError struct {
	cause error
	hint  string
}

func (e *suspendedUploadError) Error() string { return e.cause.Error() + " (" + e.hint + ")" }
func (e *suspendedUploadError) Unwrap() error { return e.cause }

// Resumable tells the CLI to print the recovery instructions when interrupted.
func (*suspendedUploadError) Resumable() {}
