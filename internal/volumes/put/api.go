package put

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// Problem type slugs the engine switches on.
const (
	slugPartTokenInvalid       = "volume-part-token-invalid"
	slugPartTokenExpired       = "volume-part-token-expired"
	slugPartLengthMismatch     = "volume-part-length-mismatch"
	slugPartStorageUnavailable = "volume-part-storage-unavailable"
	slugKeyPairNotReady        = "volume-key-pair-not-ready"
	slugLimitExceeded          = "volume-limit-exceeded"
	slugUploadNotOpen          = "volume-upload-not-open"
)

// Shared Volume limits a problem names and a LimitError reports.
const (
	limitFileBytes      = "maxFileBytes"
	limitStoredBytes    = "maxStoredBytes"
	limitFilesPerVolume = "maxFilesPerVolume"
	limitMountedBytes   = "maxMountedBytesPerAppVersion"
)

// VolumeRef names the Volume an upload addresses; an empty Environment is production.
type VolumeRef struct {
	Name        string
	Environment string
}

// UploadRef names one upload session of a Volume.
type UploadRef struct {
	Volume VolumeRef
	ID     string
}

// API is the Volumes API the engine drives; adapters keep shared auth errors and use *ProblemError for upload-specific problems.
type API interface {
	LatestFiles(ctx context.Context, volume VolumeRef) (FileList, error)
	KeyPairStatement(ctx context.Context) (KeyPairStatement, error)
	OpenUpload(ctx context.Context, volume VolumeRef, open UploadOpen) (Upload, error)
	DeclareFiles(ctx context.Context, upload UploadRef, files FileDeclarations) (DeclarationResult, error)
	RenewParts(ctx context.Context, upload UploadRef, fileID string, parts []int) ([]Target, error)
	CompleteFile(ctx context.Context, upload UploadRef, fileID string, completion FileCompletion) (UploadFile, error)
	CompleteUpload(ctx context.Context, upload UploadRef) (UploadResult, error)
	AbortUpload(ctx context.Context, upload UploadRef) error
}

// Transport sends one ciphertext part to the upload endpoint and returns its acknowledgement; PutPart never reads body after it returns.
type Transport interface {
	PutPart(ctx context.Context, target Target, body io.Reader, length int64) (Secret, error)
}

var (
	// ErrNoOrganization is put run without a configured organization id.
	ErrNoOrganization = errors.New("no organization id is configured")
	// ErrSourceChanged is a file that changed after it was hashed.
	ErrSourceChanged = errors.New("the file changed after it was hashed, so nothing from this upload was published; run put again")
	// ErrSessionExpired is a part token that needs renewing after the session deadline.
	ErrSessionExpired = errors.New("the upload session reached its deadline, which renewing a part token cannot extend; run put again")
	// ErrPartTokenInvalid is a part token the upload endpoint refused for good.
	ErrPartTokenInvalid = errors.New("the upload endpoint refused the part token")
	// ErrPartTokenExpired is a part token that passed its expiry; the engine renews it.
	ErrPartTokenExpired = errors.New("the part token expired")
	// ErrPartLengthMismatch is a part body the upload endpoint refused for its length.
	ErrPartLengthMismatch = errors.New("the upload endpoint refused the part length")
	// ErrPartStorageUnavailable is a storage failure behind the upload endpoint; it is retryable.
	ErrPartStorageUnavailable = errors.New("the upload endpoint could not store the part")
	// ErrKeyPairNotReady means the organization's key pair has not been created yet.
	ErrKeyPairNotReady = errors.New("the organization's Volumes key pair is not ready yet")
	// ErrProtocol is an answer that breaks the upload wire contract; retrying cannot fix it.
	ErrProtocol = errors.New("the answer breaks the upload contract")
)

var problemSlugs = map[error]string{
	ErrPartTokenInvalid:       slugPartTokenInvalid,
	ErrPartTokenExpired:       slugPartTokenExpired,
	ErrPartLengthMismatch:     slugPartLengthMismatch,
	ErrPartStorageUnavailable: slugPartStorageUnavailable,
	ErrKeyPairNotReady:        slugKeyPairNotReady,
}

// ProblemError is an RFC 9457 problem the Volumes API or the upload endpoint answered with.
type ProblemError struct {
	Status  int
	Slug    string
	Detail  string
	Limit   string
	Cap     int64
	Pointer string
}

func (p *ProblemError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "HTTP %d", p.Status)
	if p.Slug != "" {
		b.WriteString(" " + p.Slug)
	}
	if p.Detail != "" {
		b.WriteString(": " + p.Detail)
	}
	return b.String()
}

// Is matches the sentinel errors that name a problem type.
func (p *ProblemError) Is(target error) bool {
	slug, ok := problemSlugs[target]
	return ok && p.Slug == slug
}

type problemBody struct {
	Type   string         `json:"type"`
	Title  string         `json:"title"`
	Detail string         `json:"detail"`
	Limit  string         `json:"limit"`
	Cap    int64          `json:"cap"`
	Errors []problemField `json:"errors"`
}

type problemField struct {
	Detail  string `json:"detail"`
	Pointer string `json:"pointer"`
}

// parseProblem reads a problem+json body; a body that is not one leaves the status only.
func parseProblem(status int, body []byte) *ProblemError {
	p := &ProblemError{Status: status}
	var b problemBody
	if json.Unmarshal(body, &b) != nil {
		return p
	}
	if i := strings.LastIndex(b.Type, "#"); i >= 0 {
		p.Slug = b.Type[i+1:]
	}
	p.Detail = cmp.Or(b.Detail, b.Title)
	p.Limit, p.Cap = b.Limit, b.Cap
	if len(b.Errors) > 0 {
		p.Pointer = b.Errors[0].Pointer
	}
	return p
}

// FileError ties an error to the local file it is about.
type FileError struct {
	Path string
	Err  error
}

func (e *FileError) Error() string { return e.Path + ": " + e.Err.Error() }

func (e *FileError) Unwrap() error { return e.Err }

// sourceChangedError is ErrSourceChanged with its cause first: what no longer matches, then the advice.
type sourceChangedError struct {
	cause string
}

func (e sourceChangedError) Error() string {
	return e.cause + ", so nothing from this upload was published; run put again"
}

func (e sourceChangedError) Is(target error) bool { return target == ErrSourceChanged }

func fileErr(path string, err error) error {
	return &FileError{
		Path: path,
		Err:  err,
	}
}

// AbortFailedError is a failed upload whose session could not be aborted
// afterwards. Cause is the failure; Abort, why the abort failed, is a note
// about the session and not the failure, so a caller that reads the failure's
// API answer reads Cause.
type AbortFailedError struct {
	Cause     error
	Abort     error
	ExpiresAt time.Time
}

func (e *AbortFailedError) Error() string {
	return fmt.Sprintf("%v (could not confirm the upload was aborted; the session may stay open until %s: %v)",
		e.Cause, e.ExpiresAt.Format(time.RFC3339), e.Abort)
}

func (e *AbortFailedError) Unwrap() []error { return []error{e.Cause, e.Abort} }

// LimitError is a Shared Volume limit that an upload would go past. It
// unwraps to the API's answer that named the limit.
type LimitError struct {
	Limit string
	Cap   int64
	// Problem is the API's answer; nil when the error was built by hand.
	Problem error
}

func (e *LimitError) Unwrap() error { return e.Problem }

func (e *LimitError) Error() string {
	switch e.Limit {
	case limitFileBytes:
		return fmt.Sprintf("the file size is over the limit of %d bytes (%s)", e.Cap, e.Limit)
	case limitStoredBytes:
		return fmt.Sprintf("the upload would pass the organization's storage quota of %d bytes (%s)", e.Cap, e.Limit)
	case limitFilesPerVolume:
		return fmt.Sprintf("the Volume would hold more than %d files (%s)", e.Cap, e.Limit)
	case limitMountedBytes:
		return fmt.Sprintf("the Volume would grow past the %d bytes one app version may mount (%s)", e.Cap, e.Limit)
	default:
		return fmt.Sprintf("the upload would pass a Shared Volume limit (%s, cap %d)", e.Limit, e.Cap)
	}
}
