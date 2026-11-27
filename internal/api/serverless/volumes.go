package serverless

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/runware/runware-cli/internal/api/serverless/gen"
	"github.com/runware/runware-cli/internal/api/transport"
)

// Volume is a Shared Volume: a named set of files, its latest file set, that
// apps in its environment mount.
type Volume = gen.Volume

// VolumeCreate is the request body for createVolume.
type VolumeCreate = gen.VolumeCreate

// VolumeUpdate is the request body for updateVolume. An omitted description
// is left unchanged; a null one is cleared.
type VolumeUpdate = gen.VolumeUpdate

// VolumeDescription is a Volume's free-text description.
type VolumeDescription = gen.VolumeDescription

// VolumeID is a Volume's immutable id. A new Volume that reuses a deleted
// one's name gets a new id.
type VolumeID = gen.VolumeId

// VolumeReferences are the uses of a Volume: the ones that block a delete and
// the older app versions a confirmed delete takes the rollback from.
type VolumeReferences = gen.VolumeReferences

// VolumeReference is one app version that uses a Volume.
type VolumeReference = gen.VolumeReference

// VolumeReferenceKind is how an app version uses a Volume.
type VolumeReferenceKind = gen.VolumeReferenceKind

// The kinds of VolumeReference. Every kind but rollback-target blocks a delete.
const (
	VolumeReferenceCurrentAppVersion = gen.CurrentAppVersion
	VolumeReferenceDeployInProgress  = gen.DeployInProgress
	VolumeReferenceRunningWorker     = gen.RunningWorker
	VolumeReferenceRollbackTarget    = gen.RollbackTarget
)

// VolumeFile is one of a Volume's latest files.
type VolumeFile = gen.VolumeFile

// VolumeFilePage is a page of a Volume's latest files and the digest of the
// file list it was read from.
type VolumeFilePage = gen.VolumeFilePage

// VolumeSettings are the organization's Shared Volume access and effective
// limits.
type VolumeSettings = gen.VolumeSettings

// GetVolumeReferencesParams are the query parameters for getVolumeReferences.
type GetVolumeReferencesParams = gen.GetVolumeReferencesParams

// ListVolumesParams are the query parameters for listVolumes.
type ListVolumesParams = gen.ListVolumesParams

// ListVolumeFilesParams are the query parameters for listVolumeFiles.
type ListVolumeFilesParams = gen.ListVolumeFilesParams

// The problem types of the Shared Volume refusals a client tells apart.
const (
	ProblemVolumeInUse                        = "https://docs.runware.ai/serverless/errors#volume-in-use"
	ProblemVolumeRollbackConfirmationRequired = "https://docs.runware.ai/serverless/errors#volume-rollback-confirmation-required"
	ProblemVolumeNameTaken                    = "https://docs.runware.ai/serverless/errors#volume-name-taken"
)

// ListVolumes returns a page of the Volumes in the client's environment.
func (c *Client) ListVolumes(ctx context.Context, params ListVolumesParams) (Page[Volume], error) {
	if c.apiKey == "" {
		return Page[Volume]{}, transport.ErrNoAPIKey
	}

	params.Environment = c.environmentParam()
	resp, err := c.inner.ListVolumesWithResponse(ctx, &params)
	if err != nil {
		return Page[Volume]{}, fmt.Errorf("list volumes: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	if resp.StatusCode() != http.StatusOK {
		return Page[Volume]{}, problemFromBody(resp.Body, resp.StatusCode())
	}
	if resp.JSON200 == nil {
		return Page[Volume]{}, errors.New("list volumes: empty 200 response")
	}
	return pageOf(&resp.JSON200.Data, resp.JSON200.NextCursor), nil
}

// CreateVolume creates an empty Volume in the client's environment.
func (c *Client) CreateVolume(ctx context.Context, body VolumeCreate) (*Volume, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	params := gen.CreateVolumeParams{
		Environment: c.environmentParam(),
	}
	resp, err := c.inner.CreateVolumeWithResponse(ctx, &params, body)
	if err != nil {
		return nil, fmt.Errorf("create volume: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	if resp.StatusCode() != http.StatusCreated {
		return nil, problemFromBody(resp.Body, resp.StatusCode())
	}
	if resp.JSON201 == nil {
		return nil, errors.New("create volume: empty 201 response")
	}
	return resp.JSON201, nil
}

// GetVolume returns one Volume of the client's environment.
func (c *Client) GetVolume(ctx context.Context, name string) (*Volume, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	params := gen.GetVolumeParams{
		Environment: c.environmentParam(),
	}
	resp, err := c.inner.GetVolumeWithResponse(ctx, name, &params)
	if err != nil {
		return nil, fmt.Errorf("get volume: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	if resp.StatusCode() != http.StatusOK {
		return nil, problemFromBody(resp.Body, resp.StatusCode())
	}
	if resp.JSON200 == nil {
		return nil, errors.New("get volume: empty 200 response")
	}
	return resp.JSON200, nil
}

// UpdateVolume changes the description of one Volume of the client's
// environment.
func (c *Client) UpdateVolume(ctx context.Context, name string, body VolumeUpdate) (*Volume, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	params := gen.UpdateVolumeParams{
		Environment: c.environmentParam(),
	}
	resp, err := c.inner.UpdateVolumeWithResponse(ctx, name, &params, body)
	if err != nil {
		return nil, fmt.Errorf("update volume: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	if resp.StatusCode() != http.StatusOK {
		return nil, problemFromBody(resp.Body, resp.StatusCode())
	}
	if resp.JSON200 == nil {
		return nil, errors.New("update volume: empty 200 response")
	}
	return resp.JSON200, nil
}

// DeleteVolume logically deletes one Volume of the client's environment. id is
// the Volume's immutable id from a read, so a retry cannot delete a new Volume
// that took the name. confirmRollbackLoss accepts that older app versions
// using it lose their rollback; it never overrides a blocking use, which the
// API answers with a 409 typed ProblemVolumeInUse.
func (c *Client) DeleteVolume(ctx context.Context, name string, id VolumeID, confirmRollbackLoss bool) error {
	if c.apiKey == "" {
		return transport.ErrNoAPIKey
	}

	params := gen.DeleteVolumeParams{
		Environment: c.environmentParam(),
		VolumeId:    id,
	}
	if confirmRollbackLoss {
		params.ConfirmRollbackLoss = &confirmRollbackLoss
	}
	resp, err := c.inner.DeleteVolumeWithResponse(ctx, name, &params)
	if err != nil {
		return fmt.Errorf("delete volume: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	if resp.StatusCode() != http.StatusNoContent {
		return problemFromBody(resp.Body, resp.StatusCode())
	}
	return nil
}

// GetVolumeReferences returns a page of the uses of one Volume of the
// client's environment, blocking uses before rollback targets. The answer is a
// snapshot: it does not authorize a delete.
func (c *Client) GetVolumeReferences(ctx context.Context, name string, params GetVolumeReferencesParams) (*VolumeReferences, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	params.Environment = c.environmentParam()
	resp, err := c.inner.GetVolumeReferencesWithResponse(ctx, name, &params)
	if err != nil {
		return nil, fmt.Errorf("get volume references: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	if resp.StatusCode() != http.StatusOK {
		return nil, problemFromBody(resp.Body, resp.StatusCode())
	}
	if resp.JSON200 == nil {
		return nil, errors.New("get volume references: empty 200 response")
	}
	return resp.JSON200, nil
}

// ListVolumeFiles returns a page of the latest files of one Volume of the
// client's environment.
func (c *Client) ListVolumeFiles(ctx context.Context, name string, params ListVolumeFilesParams) (*VolumeFilePage, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	params.Environment = c.environmentParam()
	resp, err := c.inner.ListVolumeFilesWithResponse(ctx, name, &params)
	if err != nil {
		return nil, fmt.Errorf("list volume files: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	if resp.StatusCode() != http.StatusOK {
		return nil, problemFromBody(resp.Body, resp.StatusCode())
	}
	if resp.JSON200 == nil {
		return nil, errors.New("list volume files: empty 200 response")
	}
	return resp.JSON200, nil
}

// GetVolumeSettings returns the organization's Shared Volume access and
// effective limits. Settings belong to the organization, so the request
// carries no environment.
func (c *Client) GetVolumeSettings(ctx context.Context) (*VolumeSettings, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	resp, err := c.inner.GetVolumeSettingsWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("get volume settings: %w", err)
	}

	c.logResponse(ctx, resp.HTTPResponse, resp.Body)

	if resp.StatusCode() != http.StatusOK {
		return nil, problemFromBody(resp.Body, resp.StatusCode())
	}
	if resp.JSON200 == nil {
		return nil, errors.New("get volume settings: empty 200 response")
	}
	return resp.JSON200, nil
}

// volumePageSize is the page size AllVolumes and AllVolumeReferences ask
// for, the largest their routes serve.
const volumePageSize Limit = 100

// AllVolumes follows every page of the Volumes in the client's environment.
// On an error it returns no Volumes, never the pages read before it.
func (c *Client) AllVolumes(ctx context.Context) ([]Volume, error) {
	volumes := []Volume{}
	limit := volumePageSize
	var cursor *Cursor
	seen := make(map[string]struct{})
	for {
		page, err := c.ListVolumes(ctx, ListVolumesParams{
			Limit:  &limit,
			Cursor: cursor,
		})
		if err != nil {
			return nil, err
		}
		volumes = append(volumes, page.Data...)
		next, err := nextVolumeCursor(page.NextCursor, seen)
		if err != nil {
			return nil, err
		}
		if next == nil {
			return volumes, nil
		}
		cursor = next
	}
}

// AllVolumeReferences follows every page of the uses of one Volume of the
// client's environment. The result has no next cursor. On an error it returns
// no uses, never the pages read before it, which would omit some.
func (c *Client) AllVolumeReferences(ctx context.Context, name string) (*VolumeReferences, error) {
	all := &VolumeReferences{
		Blocking:        []VolumeReference{},
		RollbackTargets: []VolumeReference{},
	}
	limit := volumePageSize
	var cursor *Cursor
	seen := make(map[string]struct{})
	for {
		page, err := c.GetVolumeReferences(ctx, name, GetVolumeReferencesParams{
			Limit:  &limit,
			Cursor: cursor,
		})
		if err != nil {
			return nil, err
		}
		all.Blocking = append(all.Blocking, page.Blocking...)
		all.RollbackTargets = append(all.RollbackTargets, page.RollbackTargets...)
		next, err := nextVolumeCursor(page.NextCursor, seen)
		if err != nil {
			return nil, err
		}
		if next == nil {
			return all, nil
		}
		cursor = next
	}
}

// LatestVolumeFiles reads every page of the latest files of one Volume of the
// client's environment, starting again when the list changes between two
// pages (see latestFiles). The result has no next cursor.
func (c *Client) LatestVolumeFiles(ctx context.Context, name string) (VolumeFilePage, error) {
	limit := gen.VolumeFileLimit(volumeFilesPageSize)
	digest, files, err := latestFiles(ctx, func(ctx context.Context, cursor string) (volumeFilesPage[VolumeFile], error) {
		params := ListVolumeFilesParams{
			Limit: &limit,
		}
		if cursor != "" {
			params.Cursor = &cursor
		}
		page, err := c.ListVolumeFiles(ctx, name, params)
		if err != nil {
			return volumeFilesPage[VolumeFile]{}, err
		}
		var digest string
		if page.ManifestSha256 != nil {
			digest = *page.ManifestSha256
		}
		return volumeFilesPage[VolumeFile]{
			digest: digest,
			data:   page.Data,
			next:   page.NextCursor,
		}, nil
	}, func(err error) bool {
		re, ok := errors.AsType[*transport.RunwareError](err)
		return ok && re.StatusCode == http.StatusBadRequest
	})
	if err != nil {
		return VolumeFilePage{}, keepRetryAdvice(err)
	}
	out := VolumeFilePage{
		Data: files,
	}
	if out.Data == nil {
		out.Data = []VolumeFile{}
	}
	if digest != "" {
		out.ManifestSha256 = &digest
	}
	return out, nil
}

// keepRetryAdvice puts the advice of a listing that kept changing into the
// API answer it ended on: the CLI prints that answer's message, which alone
// would read as an unexplained 400. Any other error is returned as it is.
func keepRetryAdvice(err error) error {
	if !errors.Is(err, errVolumeFilesKeptChanging) {
		return err
	}
	re, ok := errors.AsType[*transport.RunwareError](err)
	if !ok {
		return err
	}
	api := *re
	api.Message = fmt.Sprintf("%v (the last answer: %s)", errVolumeFilesKeptChanging, re.Message)
	return &filesKeptChangingError{
		api: &api,
	}
}

// filesKeptChangingError is a listing that ran out of attempts on a later
// page's 400, whose message carries the advice to retry.
type filesKeptChangingError struct {
	api *transport.RunwareError
}

func (e *filesKeptChangingError) Error() string { return e.api.Message }

func (e *filesKeptChangingError) Unwrap() []error {
	return []error{errVolumeFilesKeptChanging, e.api}
}

// volumeFilesPage is one page of a Volume's latest files, as latestFiles
// reads it. digest is "" for a Volume with no files.
type volumeFilesPage[T any] struct {
	digest string
	data   []T
	next   *string
}

// latestFiles reads every page of a Volume's latest files with fetch, which
// takes a cursor ("" for the first page). The listing starts again, a bounded
// number of times, when the list changes between two pages: a later page
// comes from another file list, or answers the 400 a cursor issued before an
// upload completed gets, which staleCursor recognizes. When the attempts run
// out on such a 400, the error keeps it, since a 400 may be about something
// other than a stale cursor. A first page's 400 is returned as it is.
func latestFiles[T any](ctx context.Context, fetch func(context.Context, string) (volumeFilesPage[T], error), staleCursor func(error) bool) (string, []T, error) {
	var last error
	for range volumeFileListAttempts {
		digest, files, changed, err := readFilesOnce(ctx, fetch, staleCursor)
		if !changed {
			return digest, files, err
		}
		last = err
	}
	return "", nil, errors.Join(errVolumeFilesKeptChanging, last)
}

// readFilesOnce reads the pages once; changed reports that the list may have
// changed under it, with the later page's 400 as err when that is why.
func readFilesOnce[T any](ctx context.Context, fetch func(context.Context, string) (volumeFilesPage[T], error), staleCursor func(error) bool) (digest string, files []T, changed bool, err error) {
	seen := make(map[string]struct{})
	cursor := ""
	for first := true; ; first = false {
		page, err := fetch(ctx, cursor)
		if err != nil {
			return "", nil, !first && staleCursor(err), err
		}
		if first {
			digest = page.digest
		} else if page.digest != digest {
			return "", nil, true, nil
		}
		files = append(files, page.data...)
		next, err := nextVolumeCursor(page.next, seen)
		if err != nil || next == nil {
			return digest, files, false, err
		}
		cursor = *next
	}
}

// nextVolumeCursor is the cursor of the next page, or nil after the last
// page. A cursor the API repeats would loop forever, so it is an error.
func nextVolumeCursor(next *string, seen map[string]struct{}) (*string, error) {
	if next == nil || *next == "" {
		return nil, nil
	}
	if _, dup := seen[*next]; dup {
		return nil, errRepeatedVolumeCursor
	}
	seen[*next] = struct{}{}
	return next, nil
}
