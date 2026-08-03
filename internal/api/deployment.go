/**
 * [INPUT]: 依赖 client.go 的 Client.do / notFoundCode / ErrNotFound、fmt
 * [OUTPUT]: 对外提供 EnvDeployment / DeploymentOverview 类型、Client.GetDeploymentOverview(appKey) 方法
 * [POS]: internal/api 的部署服务（make-deployment）调用层，POST /deployment/v1/deployment/overview，
 *        与 client.go 的 Meta 操作共用 Client 与 do 原语；被 cmd/app_info 消费
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package api

import "fmt"

// EnvDeployment 描述单个环境（preview/production）的部署状态。
// URL 是该环境的访问地址，是 app info 命令的核心产出。
type EnvDeployment struct {
	Status         string `json:"status"`
	BuildTaskID    string `json:"buildTaskID"`
	CommitSha      string `json:"commitSha"`
	URL            string `json:"url"`
	DeploymentID   string `json:"deploymentID"`
	DesiredRelease string `json:"desiredRelease"`
	ActiveRelease  string `json:"activeRelease"`
}

// DeploymentOverview 是应用双环境部署总览。
// 环境字段用指针承载「无部署数据」：nil = 该环境从未部署，
// 让渲染层的占位分支自然落在 nil 判定上，不需要空结构体启发式。
type DeploymentOverview struct {
	TenantID   string         `json:"tenantID"`
	AppKey     string         `json:"appKey"`
	Preview    *EnvDeployment `json:"preview"`
	Production *EnvDeployment `json:"production"`
}

// GetDeploymentOverview 查询指定 App 的双环境部署总览。
// 业务码 404 表示该 App 从未部署，返回 ErrNotFound（调用方视为合法状态而非失败）；
// 其余非 200 业务码与传输/解码错误原样上抛。
func (c *Client) GetDeploymentOverview(appKey string) (*DeploymentOverview, error) {
	body := map[string]any{"appKey": appKey}
	var result struct {
		Code    int                `json:"code"`
		Message string             `json:"msg"`
		Data    DeploymentOverview `json:"data"`
	}
	if err := c.do("MakeService.GetResource", "/deployment/v1/deployment/overview", body, &result); err != nil {
		return nil, err
	}
	if result.Code == notFoundCode {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, result.Message)
	}
	if result.Code != 200 {
		return nil, fmt.Errorf("API 错误 [%d]: %s", result.Code, result.Message)
	}
	return &result.Data, nil
}
