package api

import "errors"

// ErrModelRequired is returned by Run when the model argument is empty.
var ErrModelRequired = errors.New("model is required")

// ErrModelUploadViaRun is returned by Run when the resolved task type is
// modelUpload, which must go through 'runware model upload'.
var ErrModelUploadViaRun = errors.New("modelUpload is not supported by 'run'; use 'runware model upload' instead")

// ErrModelUploadTransport is returned by ModelUpload when the transport cannot
// stream pipeline status frames (modelUpload statuses are not available via
// getResponse polling, so the HTTP transport cannot follow the upload).
var ErrModelUploadTransport = errors.New("model upload requires the WebSocket transport; rerun with --transport ws")
