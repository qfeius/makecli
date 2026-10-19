/**
 * [INPUT]: Shared HTTP boundary, resolved task identity and event sequence cursor
 * [OUTPUT]: DeploymentLogChunk and cancellable deployment process event reads
 * [POS]: Deployment events API adapter; distinct from build byte cursors and runtime logs
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
)

// DeploymentLogChunk uses the deployment event sequence as its cursor.
type DeploymentLogChunk struct {
	Content    string `json:"content"`
	NextCursor uint64 `json:"nextCursor"`
	Completed  bool   `json:"completed"`
}

// GetDeploymentLog reads events for the selected task's actual hosting app.
// Authentication, safe request diagnostics and tracing use the shared HTTP boundary.
func (c *Client) GetDeploymentLog(ctx context.Context, task BuildTask, cursor uint64) (*DeploymentLogChunk, error) {
	tenantID := strconv.FormatInt(task.OrgID, 10)
	if task.OrgID <= 0 {
		tenantID = c.headers["X-Tenant-ID"]
	}
	if task.ID <= 0 || task.AppKey == "" || tenantID == "" {
		return nil, fmt.Errorf("deployment log task is missing tenant, app or task identity")
	}
	body := struct {
		TenantID    string `json:"tenantID"`
		AppKey      string `json:"appKey"`
		BuildTaskID string `json:"buildTaskId"`
		Cursor      uint64 `json:"cursor"`
	}{tenantID, task.AppKey, strconv.FormatInt(task.ID, 10), cursor}
	var result struct {
		Code    int                 `json:"code"`
		Message string              `json:"msg"`
		Data    *DeploymentLogChunk `json:"data"`
	}
	if err := c.requestContext(ctx, http.MethodPost, "MakeService.GetResource", "/deployment/v1/deployment/events", body, &result); err != nil {
		return nil, err
	}
	if result.Code != http.StatusOK {
		return nil, fmt.Errorf("API 错误 [%d]: %s", result.Code, result.Message)
	}
	if result.Data == nil {
		return nil, fmt.Errorf("invalid deployment log response: missing data")
	}
	return result.Data, nil
}
