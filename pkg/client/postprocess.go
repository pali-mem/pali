package client

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// IngestMemory calls POST /v1/memory/ingest.
func (c *Client) IngestMemory(ctx context.Context, req StoreMemoryRequest) (IngestMemoryResponse, error) {
	var out IngestMemoryResponse
	err := c.doJSON(ctx, http.MethodPost, "/v1/memory/ingest", nil, req, &out)
	return out, err
}

// IngestMemoryBatch calls POST /v1/memory/ingest/batch.
func (c *Client) IngestMemoryBatch(ctx context.Context, req StoreMemoryBatchRequest) (IngestMemoryResponse, error) {
	var out IngestMemoryResponse
	err := c.doJSON(ctx, http.MethodPost, "/v1/memory/ingest/batch", nil, req, &out)
	return out, err
}

// GetPostprocessJob calls GET /v1/memory/jobs/:id.
func (c *Client) GetPostprocessJob(ctx context.Context, jobID string) (PostprocessJobResponse, error) {
	var out PostprocessJobResponse
	path := fmt.Sprintf("/v1/memory/jobs/%s", url.PathEscape(strings.TrimSpace(jobID)))
	err := c.doJSON(ctx, http.MethodGet, path, nil, nil, &out)
	return out, err
}

// ListPostprocessJobs calls GET /v1/memory/jobs with optional filters.
func (c *Client) ListPostprocessJobs(ctx context.Context, req ListPostprocessJobsRequest) (ListPostprocessJobsResponse, error) {
	var out ListPostprocessJobsResponse
	q := make(url.Values, 4)
	q.Set("tenant_id", strings.TrimSpace(req.TenantID))
	if req.Limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", req.Limit))
	}
	if csv := joinCSV(req.Statuses); csv != "" {
		q.Set("status", csv)
	}
	if csv := joinCSV(req.Types); csv != "" {
		q.Set("type", csv)
	}
	err := c.doJSON(ctx, http.MethodGet, "/v1/memory/jobs", q, nil, &out)
	return out, err
}

func joinCSV(values []string) string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return strings.Join(out, ",")
}
