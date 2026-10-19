/**
 * [INPUT]: Shared context-aware HTTP client, build task ID, opaque cursor and first-read tail
 * [OUTPUT]: BuildLogChunk, BuildLogOptions and context-aware log readers
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
	TailLines  *int64 `json:"tailLines,omitempty"`
}

// BuildLogOptions controls either a cursor resume or an initial server-side tail.
type BuildLogOptions struct {
	Cursor    string
	TailLines *int64
}

// GetBuildLog reads the selected task without changing build or deployment state.
// Authentication, tracing and optional debug logging use the shared HTTP boundary.
func (c *Client) GetBuildLog(ctx context.Context, taskID int64, cursor string) (*BuildLogChunk, error) {
	return c.GetBuildLogWithOptions(ctx, taskID, BuildLogOptions{Cursor: cursor})
}

// GetBuildLogWithOptions preserves byte cursors when the server truncates history.
func (c *Client) GetBuildLogWithOptions(ctx context.Context, taskID int64, opts BuildLogOptions) (*BuildLogChunk, error) {
	if opts.TailLines != nil && (*opts.TailLines < 0 || opts.Cursor != "") {
		return nil, fmt.Errorf("tailLines must be non-negative and cannot be combined with cursor")
	}
	body := struct {
		BuildTaskID string `json:"buildTaskId"`
		Scope       string `json:"scope"`
		Cursor      string `json:"cursor,omitempty"`
		TailLines   *int64 `json:"tailLines,omitempty"`
	}{BuildTaskID: strconv.FormatInt(taskID, 10), Scope: "BUILD", Cursor: opts.Cursor, TailLines: opts.TailLines}
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
	if opts.TailLines != nil && (result.Data.TailLines == nil || *result.Data.TailLines != *opts.TailLines) {
		return nil, fmt.Errorf("build log server does not support --tail; upgrade the build service or use --tail=-1")
	}
	return result.Data, nil
}
