/**
 * [INPUT]: 复用 client.go 的 Client.do / checkGetResult / ErrNotFound
 * [OUTPUT]: 对外提供 BuildTask 类型（含 Finished/Succeeded 终态判定方法）、BuildStatusSuccess/Failed/Canceled 终态常量、GetBuildTask(commitSha) 方法
 * [POS]: internal/api 的构建服务（make-build-service）查询层：按 commitSha 精确查询构建任务详情
 *        （POST /build/v1/build + X-Make-Target: MakeService.GetResource，网关前缀 /api/make 由 cmd 层补齐），
 *        供 `app deploy --status` 用本地 HEAD sha 反查部署进度
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package api

// BuildTask 是构建服务返回的构建任务详情（字段契约见 make-build-service 设计文档 §11）。
// 只收录 CLI 呈现所需字段；可选字段无值时服务端不返回，零值即「无」。
// ID 是 Build→Deploy 全链路的关联键（后续日志接口按它定位）。
type BuildTask struct {
	ID                int64  `json:"id"`
	AppKey            string `json:"appKey"`
	DeploymentVersion string `json:"deploymentVersion"`
	Environment       string `json:"environment"`
	Ref               string `json:"ref"`
	CommitSha         string `json:"commitSha"`
	CommitMessage     string `json:"commitMessage"`
	Status            string `json:"status"` // PENDING | RUNNING | SUCCESS | FAILED | CANCELED
	Phase             string `json:"phase"`  // RECEIVE | CLONE | DETECT | BUILD | PUSH | DEPLOY
	Image             string `json:"image"`
	ErrorCode         string `json:"errorCode"`
	ErrorMessage      string `json:"errorMessage"`
	CreateTime        string `json:"createTime"`
	StartTime         string `json:"startTime"`
	FinishTime        string `json:"finishTime"`
}

// 构建任务终态常量（status 全集另含 PENDING / RUNNING 两个进行态，无需具名——
// 调用方只关心「到没到终态」与「成没成功」两问，进行态从不被单独判等）。
const (
	BuildStatusSuccess  = "SUCCESS"
	BuildStatusFailed   = "FAILED"
	BuildStatusCanceled = "CANCELED"
)

// Finished 报告任务是否已达终态——`app deploy --wait` 轮询的停止条件。
// 状态机知识收口在 api 层，cmd 层不做状态字符串判等。
func (t *BuildTask) Finished() bool {
	switch t.Status {
	case BuildStatusSuccess, BuildStatusFailed, BuildStatusCanceled:
		return true
	default:
		return false
	}
}

// Succeeded 报告任务是否成功收尾（FAILED 与 CANCELED 都算未成功）。
func (t *BuildTask) Succeeded() bool {
	return t.Status == BuildStatusSuccess
}

// GetBuildTask 调用 MakeService.GetResource 按 commitSha 精确查询构建任务详情。
// deploy 推送的是本地 HEAD，服务端 webhook 以 push 的 commit sha 建任务，
// 故 HEAD sha 即天然的任务定位键，无需用户抄任务 ID。
// 任务不存在（尚未 deploy / webhook 未创建任务）返回 ErrNotFound；其余错误原样返回。
func (c *Client) GetBuildTask(commitSha string) (*BuildTask, error) {
	reqBody := map[string]any{"commitSha": commitSha}
	var result struct {
		Code    int       `json:"code"`
		Message string    `json:"msg"`
		Data    BuildTask `json:"data"`
	}
	if err := c.do("MakeService.GetResource", "/build/v1/build", reqBody, &result); err != nil {
		return nil, err
	}
	if err := checkGetResult(result.Code, result.Message, result.Data.ID != 0); err != nil {
		return nil, err
	}
	return &result.Data, nil
}
