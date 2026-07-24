package serverless

import "context"

// gpuTypesResponse is the envelope returned by GET /v1/gpu-types.
type gpuTypesResponse struct {
	Data []GpuType `json:"data"`
}

// ListGpuTypes returns the catalogue of supported GPU types and their pricing.
// This endpoint is not workspace-scoped.
func (c *Client) ListGpuTypes(ctx context.Context) ([]GpuType, error) {
	var resp gpuTypesResponse
	if err := c.get(ctx, "/v1/gpu-types", &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}
