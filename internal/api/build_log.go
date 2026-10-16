/**
 * [INPUT]: Shared context-aware HTTP client, build task ID and opaque cursor
 * [OUTPUT]: BuildLogChunk and Client.GetBuildLog
 * [POS]: Build log API adapter; does not infer task success from completed
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
)

// BuildLogChunk is one incremental read of the BUILD log object.
// Empty content is valid while a log is being created or has no new bytes.
type BuildLogChunk struct {
	Content    string `json:"content"`
	NextCursor string `json:"nextCursor"`
	Completed  bool   `json:"completed"`
}

// GetBuildLog reads the selected task without changing build or deployment state.
// Authentication, tracing and optional debug logging use the shared HTTP boundary.
func (c *Client) GetBuildLog(ctx context.Context, taskID int64, cursor string) (*BuildLogChunk, error) {
	body := struct {
		BuildTaskID string `json:"buildTaskId"`
		Scope       string `json:"scope"`
		Cursor      string `json:"cursor,omitempty"`
	}{BuildTaskID: strconv.FormatInt(taskID, 10), Scope: "BUILD", Cursor: cursor}
	var result struct {
		Code    int            `json:"code"`
		Message string         `json:"msg"`
		Data    *BuildLogChunk `json:"data"`
	}
	if err := c.requestContext(ctx, http.MethodPost, "MakeService.GetResource", "/build/v1/build-log", body, &result); err != nil {
		return nil, err
	}
	if result.Code == notFoundCode {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, result.Message)
	}
	if result.Code != http.StatusOK {
		return nil, fmt.Errorf("API 错误 [%d]: %s", result.Code, result.Message)
	}
	if result.Data == nil {
		return nil, fmt.Errorf("invalid build log response: missing data")
	}
	return result.Data, nil
}
