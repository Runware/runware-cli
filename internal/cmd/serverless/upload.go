package serverless

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
	serverlessapi "github.com/runware/runware-cli/internal/api/serverless"
)

// uploadSource stages a source archive and returns the id of the source the
// ready upload published, for the create request to consume.
//
// Three steps, because the archive never travels through the API itself: the
// session declares what is coming, the bytes go straight to the staging object
// the API names, and completion is what opens the archive and verifies it
// against the declaration. Only then does the published source mean anything to
// a create.
func uploadSource(ctx context.Context, client *serverlessapi.Client, archive []byte) (uuid.UUID, error) {
	digest := sha256.Sum256(archive)

	created, err := client.CreateSourceUpload(ctx, serverlessapi.SourceUploadCreate{
		DeclaredByteLength: int64(len(archive)),
		// Fresh per invocation, and deliberately not the archive's digest: a
		// session replays only while it is still pending, and answers 409 once
		// it is ready or consumed. Keyed on content, a tree that deployed once
		// could never be deployed again.
		IdempotencyKey: uuid.NewString(),
		Sha256:         hex.EncodeToString(digest[:]),
		SourceType:     serverlessapi.AppSourceTypeCode,
	})
	if err != nil {
		return uuid.Nil, err
	}

	if err := client.StageSourceArchive(ctx, created.Transfer, archive); err != nil {
		// The session holds a staging object that will now never be completed,
		// so give it back rather than leaving it to expire. A failure here is
		// not the one worth reporting.
		_ = client.DeleteSourceUpload(ctx, created.Upload.Id)
		return uuid.Nil, err
	}

	upload, err := client.CompleteSourceUpload(ctx, created.Upload.Id)
	if err != nil {
		return uuid.Nil, err
	}
	if upload.State != serverlessapi.SourceUploadStateReady {
		return uuid.Nil, fmt.Errorf("source upload %s: %s", upload.State, rejectionReason(upload))
	}
	if upload.SourceId == nil {
		return uuid.Nil, fmt.Errorf("source upload %s: ready without a source id", upload.Id)
	}
	return *upload.SourceId, nil
}

// rejectionReason reports why completion refused an archive, for the states
// that carry one.
func rejectionReason(upload *serverlessapi.SourceUpload) string {
	if upload.RejectionReason == nil || *upload.RejectionReason == "" {
		return "no reason given"
	}
	return *upload.RejectionReason
}
