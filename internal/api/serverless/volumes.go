package serverless

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"github.com/runware/runware-cli/internal/api/serverless/gen"
	"github.com/runware/runware-cli/internal/api/transport"
)

// Volume is a Shared Volume: a named set of files, its latest file set, that
// apps in its environment mount.
type Volume = gen.Volume

// VolumeNamePattern is a Volume name the API accepts.
var VolumeNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)

// ErrVolumeName rejects a name VolumeNamePattern does not match.
var ErrVolumeName = errors.New("volume name must be 1-63 lowercase letters, digits, hyphens, underscores or periods, starting with a letter or digit")

// VolumeCreate is the request body for createVolume.
type VolumeCreate = gen.VolumeCreate

// VolumeUpdate is the request body for updateVolume. An omitted description
// is left unchanged; a null one is cleared.
type VolumeUpdate = gen.VolumeUpdate

// VolumeDescription is a Volume's free-text description.
type VolumeDescription = gen.VolumeDescription

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

// ListVolumesParams are the query parameters for listVolumes.
type ListVolumesParams = gen.ListVolumesParams

// VolumeFileLimit is the page size of listVolumeFiles.
type VolumeFileLimit = gen.VolumeFileLimit

// ListVolumeFilesParams are the query parameters for listVolumeFiles.
type ListVolumeFilesParams = gen.ListVolumeFilesParams

// The problem types of the Shared Volume refusals a client tells apart.
const (
	ProblemVolumeInUse                        = "https://docs.runware.ai/serverless/errors#volume-in-use"
	ProblemVolumeRollbackConfirmationRequired = "https://docs.runware.ai/serverless/errors#volume-rollback-confirmation-required"
	ProblemVolumeNameTaken                    = "https://docs.runware.ai/serverless/errors#volume-name-taken"
	ProblemVolumeLimitExceeded                = "https://docs.runware.ai/serverless/errors#volume-limit-exceeded"
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

// DeleteVolume logically deletes one Volume of the client's environment.
// confirmRollbackLoss accepts that older app versions using it lose their
// rollback; it never overrides a blocking use, which the API answers with a
// 409 typed ProblemVolumeInUse.
func (c *Client) DeleteVolume(ctx context.Context, name string, confirmRollbackLoss bool) error {
	if c.apiKey == "" {
		return transport.ErrNoAPIKey
	}

	params := gen.DeleteVolumeParams{
		Environment: c.environmentParam(),
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

// GetVolumeReferences lists the uses of one Volume of the client's
// environment. The answer is a snapshot: it does not authorize a delete.
func (c *Client) GetVolumeReferences(ctx context.Context, name string) (*VolumeReferences, error) {
	if c.apiKey == "" {
		return nil, transport.ErrNoAPIKey
	}

	params := gen.GetVolumeReferencesParams{
		Environment: c.environmentParam(),
	}
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
