package serverless

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/runware/runware-cli/internal/api/serverless/gen"
	"github.com/runware/runware-cli/internal/api/transport"
)

// SourceUpload is one upload session for an app's source archive.
type SourceUpload = gen.SourceUpload

// SourceUploadCreate declares the archive a session is opened for.
type SourceUploadCreate = gen.SourceUploadCreate

// SourceUploadCreation is a new session plus the transfer instruction for it.
type SourceUploadCreation = gen.SourceUploadCreation

// SourceUploadID identifies an upload session.
type SourceUploadID = gen.SourceUploadId

// SourceUploadState is the lifecycle state of an upload session.
type SourceUploadState = gen.SourceUploadState

// SourceUploadTransfer is the instruction for staging one archive.
type SourceUploadTransfer = gen.SourceUploadTransfer

// transferTimeout bounds the archive transfer, which is the one request in a
// deploy that carries the whole codebase.
const transferTimeout = 5 * time.Minute

// SourceUploadStateReady is the state a session reaches once completion has
// verified the staged archive against the declaration.
const SourceUploadStateReady = gen.SourceUploadStateReady

// CreateSourceUpload opens an upload session for appId, which need not exist
// yet, and returns it with a short-lived instruction for staging the archive.
func (c *Client) CreateSourceUpload(ctx context.Context, appID string, body SourceUploadCreate) (*SourceUploadCreation, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.CreateSourceUploadWithResponse(ctx, appID, body)
	if err != nil {
		return nil, fmt.Errorf("create source upload: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	switch resp.StatusCode() {
	case http.StatusCreated:
		if resp.JSON201 == nil {
			return nil, fmt.Errorf("create source upload: empty 201 response")
		}
		return resp.JSON201, nil
	case http.StatusBadRequest:
		return nil, problemToError(resp.ApplicationproblemJSON400, http.StatusBadRequest)
	case http.StatusUnauthorized:
		return nil, problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	case http.StatusForbidden:
		return nil, problemToError(resp.ApplicationproblemJSON403, http.StatusForbidden)
	case http.StatusConflict:
		return nil, problemToError(resp.ApplicationproblemJSON409, http.StatusConflict)
	case http.StatusUnprocessableEntity:
		return nil, problemToError(resp.ApplicationproblemJSON422, http.StatusUnprocessableEntity)
	default:
		return nil, problemFromBody(resp.Body, resp.StatusCode())
	}
}

// StageSourceArchive sends the archive to the staging object the transfer
// instruction names.
//
// The request is issued through the bare doer rather than the generated client:
// the URL carries its own signature, and the Authorization header the generated
// client attaches to every call would invalidate it. Only the headers the
// instruction lists are sent.
func (c *Client) StageSourceArchive(ctx context.Context, transfer SourceUploadTransfer, archive []byte) error {
	put, err := transfer.AsSourceUploadSinglePutTransfer()
	if err != nil {
		return fmt.Errorf("stage source archive: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, string(put.Method), put.Url, bytes.NewReader(archive))
	if err != nil {
		return fmt.Errorf("stage source archive: %w", err)
	}
	for name, value := range put.Headers {
		req.Header.Set(name, value)
	}
	// Set explicitly: a bytes.Reader body would otherwise be sent chunked, and
	// completion verifies the staged object's exact length.
	req.ContentLength = int64(len(archive))

	resp, err := c.transferDoer().Do(req)
	if err != nil {
		return fmt.Errorf("stage source archive: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("stage source archive: %s", resp.Status)
	}
	return nil
}

// CompleteSourceUpload asks the API to verify the staged archive against the
// session declaration and returns the session in its settled state.
func (c *Client) CompleteSourceUpload(ctx context.Context, appID string, uploadID SourceUploadID) (*SourceUpload, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.CompleteSourceUploadWithResponse(ctx, appID, uploadID)
	if err != nil {
		return nil, fmt.Errorf("complete source upload: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	switch resp.StatusCode() {
	case http.StatusOK:
		if resp.JSON200 == nil {
			return nil, fmt.Errorf("complete source upload: empty 200 response")
		}
		return resp.JSON200, nil
	case http.StatusBadRequest:
		return nil, problemToError(resp.ApplicationproblemJSON400, http.StatusBadRequest)
	case http.StatusUnauthorized:
		return nil, problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	case http.StatusForbidden:
		return nil, problemToError(resp.ApplicationproblemJSON403, http.StatusForbidden)
	case http.StatusNotFound:
		return nil, problemToError(resp.ApplicationproblemJSON404, http.StatusNotFound)
	case http.StatusConflict:
		return nil, problemToError(resp.ApplicationproblemJSON409, http.StatusConflict)
	case http.StatusUnprocessableEntity:
		return nil, problemToError(resp.ApplicationproblemJSON422, http.StatusUnprocessableEntity)
	default:
		return nil, problemFromBody(resp.Body, resp.StatusCode())
	}
}

// DeleteSourceUpload aborts an unconsumed session and removes its staging
// object. Repeating a successful abort is idempotent.
func (c *Client) DeleteSourceUpload(ctx context.Context, appID string, uploadID SourceUploadID) error {
	if c.apiKey == "" {
		return transport.ErrNoAPIKey
	}

	resp, err := c.inner.DeleteSourceUploadWithResponse(ctx, appID, uploadID)
	if err != nil {
		return fmt.Errorf("delete source upload: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	switch resp.StatusCode() {
	case http.StatusNoContent:
		return nil
	case http.StatusBadRequest:
		return problemToError(resp.ApplicationproblemJSON400, http.StatusBadRequest)
	case http.StatusUnauthorized:
		return problemToError(resp.ApplicationproblemJSON401, http.StatusUnauthorized)
	case http.StatusForbidden:
		return problemToError(resp.ApplicationproblemJSON403, http.StatusForbidden)
	case http.StatusNotFound:
		return problemToError(resp.ApplicationproblemJSON404, http.StatusNotFound)
	case http.StatusConflict:
		return problemToError(resp.ApplicationproblemJSON409, http.StatusConflict)
	case http.StatusUnprocessableEntity:
		return problemToError(resp.ApplicationproblemJSON422, http.StatusUnprocessableEntity)
	default:
		return problemFromBody(resp.Body, resp.StatusCode())
	}
}

// transferDoer returns a doer with the archive transfer's longer deadline. The
// same reasoning as createInner, for the request that now carries the bytes.
func (c *Client) transferDoer() gen.HttpRequestDoer {
	hc, ok := c.doer.(*http.Client)
	if !ok {
		return c.doer
	}
	if hc.Timeout == 0 || hc.Timeout >= transferTimeout {
		return hc
	}
	cloned := *hc
	cloned.Timeout = transferTimeout
	return &cloned
}
