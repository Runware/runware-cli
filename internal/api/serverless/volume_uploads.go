package serverless

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/runware/runware-cli/internal/agents"
	"github.com/runware/runware-cli/internal/api/transport"
	"github.com/runware/runware-cli/internal/buildinfo"
	"github.com/runware/runware-cli/internal/volumes/put"
)

const (
	// volumeFilesPageSize is the largest page listVolumeFiles serves.
	volumeFilesPageSize = 1000
	// volumeFileListAttempts bounds how often LatestFiles starts again because
	// an upload changed the Volume's files between two pages.
	volumeFileListAttempts = 3
	// maxVolumeAnswerBytes bounds a Volumes API answer the client reads.
	maxVolumeAnswerBytes = 16 << 20
	// volumeCompletionTimeout bounds completeVolumeUploadFile and
	// completeVolumeUpload. Completing a file completes its object in storage
	// and checks its checksum, which can take minutes for a large file, and a
	// retry after a completion whose answer was lost can fail the file for
	// good (volume-upload-completion-unrecoverable), so these calls must not
	// time out at the client's default.
	volumeCompletionTimeout = 5 * time.Minute
	problemTypeSeparator    = "#"
)

var (
	errVolumeFilesKeptChanging = errors.New("the Volume's files kept changing while they were listed; retry once uploads to it finish")
	errRepeatedVolumeCursor    = errors.New("the API returned the same page cursor twice; stopped listing")
)

// volumeSecretKeys are the JSON members of a Volumes answer that carry a data
// key, a part token, an acknowledgement or an upload address. The debug log
// replaces them, on success and problem answers alike, in any letter case.
var volumeSecretKeys = map[string]struct{}{
	"value":         {},
	"token":         {},
	"ack":           {},
	"url":           {},
	"sealedKey":     {},
	"dataKey":       {},
	"encapsulation": {},
}

// volumeBodyOmitted stands in the debug log for a body that is not JSON.
const volumeBodyOmitted = "[omitted: not JSON]"

// VolumeUploads drives the Volumes upload routes for the put engine.
type VolumeUploads struct {
	c *Client
}

var _ put.API = (*VolumeUploads)(nil)

// VolumeUploads returns the put.API on this client's key, base URL and logger.
// Each call names its own environment, so the client's environment is not used.
func (c *Client) VolumeUploads() *VolumeUploads {
	return &VolumeUploads{c: c}
}

// volumeFilePage is one page of listVolumeFiles.
type volumeFilePage struct {
	ManifestSHA256 string           `json:"manifestSha256"`
	Data           []put.VolumeFile `json:"data"`
	NextCursor     *string          `json:"nextCursor"`
}

// LatestFiles reads every page of the Volume's latest files, starting again
// when the list changes between two pages (see latestFiles).
func (v *VolumeUploads) LatestFiles(ctx context.Context, volume put.VolumeRef) (put.FileList, error) {
	digest, files, err := latestFiles(ctx, func(ctx context.Context, cursor string) (volumeFilesPage[put.VolumeFile], error) {
		query := url.Values{}
		query.Set("limit", strconv.Itoa(volumeFilesPageSize))
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		var page volumeFilePage
		err := v.do(ctx, http.MethodGet, volumePath(volume.Name, "files"), volume.Environment, query, nil, http.StatusOK, &page)
		return volumeFilesPage[put.VolumeFile]{
			digest: page.ManifestSHA256,
			data:   page.Data,
			next:   page.NextCursor,
		}, err
	}, func(err error) bool {
		p, ok := errors.AsType[*put.ProblemError](err)
		return ok && p.Status == http.StatusBadRequest
	})
	if err != nil {
		return put.FileList{}, err
	}
	return put.FileList{
		ManifestSHA256: digest,
		Files:          files,
	}, nil
}

// KeyPairStatement reads the organization's signed key-pair statement.
func (v *VolumeUploads) KeyPairStatement(ctx context.Context) (put.KeyPairStatement, error) {
	var st put.KeyPairStatement
	err := v.do(ctx, http.MethodGet, "v1/volumes-key-pair", "", nil, nil, http.StatusOK, &st)
	return st, err
}

// OpenUpload opens an upload session on the Volume.
func (v *VolumeUploads) OpenUpload(ctx context.Context, volume put.VolumeRef, open put.UploadOpen) (put.Upload, error) {
	var up put.Upload
	err := v.do(ctx, http.MethodPost, volumePath(volume.Name, "uploads"), volume.Environment, nil, open, http.StatusCreated, &up)
	return up, err
}

// DeclareFiles declares a batch of files; the answer carries the first part targets.
func (v *VolumeUploads) DeclareFiles(ctx context.Context, upload put.UploadRef, files put.FileDeclarations) (put.DeclarationResult, error) {
	var res put.DeclarationResult
	err := v.do(ctx, http.MethodPost, uploadPath(upload, "files"), upload.Volume.Environment, nil, files, http.StatusOK, &res)
	return res, err
}

// RenewParts asks for fresh targets for the named parts of a file.
func (v *VolumeUploads) RenewParts(ctx context.Context, upload put.UploadRef, fileID string, parts []int) ([]put.Target, error) {
	var res put.UploadTargets
	body := put.PartTokenRequest{PartNumbers: parts}
	err := v.do(ctx, http.MethodPost, uploadPath(upload, "files", fileID, "parts"), upload.Volume.Environment, nil, body, http.StatusOK, &res)
	return res.UploadTargets, err
}

// CompleteFile completes one file with its ciphertext checksum and part acknowledgements.
func (v *VolumeUploads) CompleteFile(ctx context.Context, upload put.UploadRef, fileID string, completion put.FileCompletion) (put.UploadFile, error) {
	var f put.UploadFile
	err := v.withMinTimeout(volumeCompletionTimeout).do(ctx, http.MethodPost, uploadPath(upload, "files", fileID, "complete"), upload.Volume.Environment, nil, completion, http.StatusOK, &f)
	return f, err
}

// CompleteUpload applies the session to the Volume's latest files.
func (v *VolumeUploads) CompleteUpload(ctx context.Context, upload put.UploadRef) (put.UploadResult, error) {
	var res put.UploadResult
	err := v.withMinTimeout(volumeCompletionTimeout).do(ctx, http.MethodPost, uploadPath(upload, "complete"), upload.Volume.Environment, nil, nil, http.StatusOK, &res)
	return res, err
}

// AbortUpload aborts the session.
func (v *VolumeUploads) AbortUpload(ctx context.Context, upload put.UploadRef) error {
	return v.do(ctx, http.MethodDelete, uploadPath(upload), upload.Volume.Environment, nil, nil, http.StatusNoContent, nil)
}

// withMinTimeout returns the adapter on a copy of the client whose HTTP
// client allows each request at least d. Only do reads the copy, and do sends
// through its doer, not its generated client.
func (v *VolumeUploads) withMinTimeout(d time.Duration) *VolumeUploads {
	c := *v.c
	c.doer = doerWithMinTimeout(c.doer, d)
	return &VolumeUploads{c: &c}
}

func volumePath(name string, rest ...string) string {
	return joinPath(append([]string{"v1", "volumes", name}, rest...)...)
}

func uploadPath(upload put.UploadRef, rest ...string) string {
	return volumePath(upload.Volume.Name, append([]string{"uploads", upload.ID}, rest...)...)
}

// joinPath escapes each segment, so a name cannot add or climb a path level.
func joinPath(segments ...string) string {
	escaped := make([]string, len(segments))
	for i, s := range segments {
		escaped[i] = url.PathEscape(s)
		if s == "." || s == ".." {
			// PathEscape keeps dots, and a dot segment would climb a level.
			escaped[i] = strings.ReplaceAll(s, ".", "%2E")
		}
	}
	return strings.Join(escaped, "/")
}

// do sends one Volumes request and decodes a want answer into out; any other
// status is a *VolumeAPIError, which unwraps to a *put.ProblemError.
func (v *VolumeUploads) do(ctx context.Context, method, path, environment string, query url.Values, body any, want int, out any) error {
	c := v.c
	if c.apiKey == "" {
		return transport.ErrNoAPIKey
	}
	if environment != "" {
		if query == nil {
			query = url.Values{}
		}
		query.Set("environment", environment)
	}
	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode the request: %w", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return fmt.Errorf("build the request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("User-Agent", userAgent())
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.doer.Do(req)
	if err != nil {
		// The url.Error text repeats the request address; keep only its cause.
		if ue, ok := errors.AsType[*url.Error](err); ok {
			err = ue.Err
		}
		return fmt.Errorf("%s %s: %w", method, req.URL.Path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	answer, err := io.ReadAll(io.LimitReader(resp.Body, maxVolumeAnswerBytes))
	if err != nil {
		return fmt.Errorf("read the answer: %w", err)
	}
	v.logResponse(ctx, resp, answer)
	if resp.StatusCode != want {
		return newVolumeAPIError(resp.StatusCode, answer)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(answer, out); err != nil {
		return fmt.Errorf("%w: the %s %s answer is not the expected JSON", put.ErrProtocol, method, req.URL.Path)
	}
	return nil
}

func (v *VolumeUploads) logResponse(ctx context.Context, resp *http.Response, body []byte) {
	logger := v.c.logger
	if logger == nil || !logger.Enabled(ctx, slog.LevelDebug) {
		return
	}
	path, status := responsePathStatus(resp)
	// A body that is not JSON, such as one cut at maxVolumeAnswerBytes,
	// cannot be checked for secrets, so it is left out of the log.
	logged := ""
	if len(body) > 0 {
		logged = volumeBodyOmitted
		if redacted, ok := redactJSONKeys(body, volumeSecretKeys); ok {
			logged = string(redacted)
		}
	}
	logger.Debug("serverless response", //nolint:errcheck,gosec
		"path", path,
		"status", status,
		"bodyBytes", len(body),
		"body", logged,
	)
}

// VolumeAPIError is a problem the Volumes API answered, as opposed to the
// upload endpoint a part is sent to. It unwraps to the *put.ProblemError the
// engine reads, so a caller can tell an API refusal (the API key, the Volume)
// from a refused part, and to the *transport.RunwareError every other
// serverless call returns, so the CLI prints it with the API's fields.
type VolumeAPIError struct {
	Problem *put.ProblemError
	API     *transport.RunwareError
}

func newVolumeAPIError(status int, body []byte) *VolumeAPIError {
	// problemFromBody always returns a *transport.RunwareError.
	api, _ := errors.AsType[*transport.RunwareError](problemFromBody(body, status))
	return &VolumeAPIError{
		Problem: volumeProblem(status, body),
		API:     api,
	}
}

func (e *VolumeAPIError) Error() string { return e.Problem.Error() }

func (e *VolumeAPIError) Unwrap() []error {
	if e.API == nil {
		return []error{e.Problem}
	}
	return []error{e.Problem, e.API}
}

// volumeProblemBody is the part of an RFC 9457 problem the put engine reads.
type volumeProblemBody struct {
	Type   string               `json:"type"`
	Title  string               `json:"title"`
	Detail string               `json:"detail"`
	Limit  string               `json:"limit"`
	Cap    int64                `json:"cap"`
	Errors []volumeProblemField `json:"errors"`
}

type volumeProblemField struct {
	Pointer string `json:"pointer"`
}

// volumeProblem reads a problem answer into the engine's error: the status,
// the slug of the problem type, the detail, the limit and cap, and the first
// field pointer. A body that is not a problem keeps the status and its text.
func volumeProblem(status int, body []byte) *put.ProblemError {
	p := &put.ProblemError{
		Status: status,
		Detail: http.StatusText(status),
	}
	var b volumeProblemBody
	if json.Unmarshal(body, &b) != nil {
		return p
	}
	if i := strings.LastIndex(b.Type, problemTypeSeparator); i >= 0 {
		p.Slug = b.Type[i+len(problemTypeSeparator):]
	}
	p.Detail = cmp.Or(b.Detail, b.Title, p.Detail)
	p.Limit, p.Cap = b.Limit, b.Cap
	if len(b.Errors) > 0 {
		p.Pointer = b.Errors[0].Pointer
	}
	return p
}

// userAgent is the User-Agent of every serverless request.
func userAgent() string {
	ua := buildinfo.UserAgent()
	if agent := agents.Detect(); agent != "" {
		ua += " agent/" + string(agent)
	}
	return ua
}
