package put

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// fail ends a session, fresh or resumed, after a failed transfer: an interrupt keeps it open for a resume,
// any other failure aborts it. An interrupt once the completion was sent also aborts, since the abort is
// what tells whether the session completed.
func (r *runner) fail(ctx context.Context, err error) error {
	cause := context.Cause(ctx)
	if cause != nil && (errors.Is(err, ctx.Err()) || errors.Is(err, cause)) {
		err = cause
		if !r.completionAttempted {
			return r.suspend(cause)
		}
	}
	// A fresh put is the next step unless the completion request was sent, which may have published the files.
	rerun := errors.Is(err, errUploadRetriesExhausted) && !r.completionAttempted
	err = r.abort(ctx, err)
	if rerun {
		err = fmt.Errorf("%w; run put again", err)
	}
	return err
}

// suspend leaves the session open and its resume state on disk, so running put again with the same paths resumes it.
func (r *runner) suspend(cause error) error {
	if r.journal != nil {
		if err := r.journal.close(); err != nil {
			r.warn("%v", err)
		}
	}
	return fmt.Errorf("%w (the upload session stays open until %s: run put again with the same paths to resume it)",
		cause, r.expiresAt.Format(time.RFC3339))
}
