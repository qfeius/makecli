/**
 * [INPUT]: Shared authenticated HTTP client, trace ID and page number
 * [OUTPUT]: Runtime log page from the Console trace query
 * [POS]: Read-only adapter for the planned traceId-only Console API
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */
package api

import (
	"context"
	"fmt"
	"net/http"
)

// RuntimeLogPage retains the server's log fields and pagination metadata.
type RuntimeLogPage struct {
	Data       []map[string]any `json:"data"`
	Pagination struct {
		Page  int `json:"page"`
		Size  int `json:"size"`
		Total int `json:"total"`
	} `json:"pagination"`
}

// ListRuntimeLogs fetches one page without imposing app or time constraints.
func (c *Client) ListRuntimeLogs(ctx context.Context, traceID string, page, size int) (*RuntimeLogPage, error) {
	body := map[string]any{
		"filter":     map[string]string{"expression": "trace_id = '" + traceID + "' AND event_type = 'make_runtime'"},
		"pagination": map[string]int{"page": page, "size": size},
		"sortOrder":  "asc",
	}
	var result struct {
		Code    int    `json:"code"`
		Message string `json:"msg"`
		RuntimeLogPage
	}
	if err := c.requestContext(ctx, http.MethodPost, "MakeService.ListResources", "/console/v1/log", body, &result); err != nil {
		return nil, err
	}
	if result.Code != http.StatusOK {
		return nil, fmt.Errorf("API 错误 [%d]: %s", result.Code, result.Message)
	}
	if result.Data == nil || result.Pagination.Page != page || result.Pagination.Size != size || result.Pagination.Total < 0 {
		return nil, fmt.Errorf("invalid runtime log response: missing data or pagination")
	}
	return &result.RuntimeLogPage, nil
}
