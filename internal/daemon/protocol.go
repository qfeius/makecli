/**
 * [INPUT]: 依赖 encoding/json、time；线上形状真相源是 agent-design/Contract.md（黄金测试锁在 agent-contract 仓库）
 * [OUTPUT]: 对外提供 daemon 协议的 wire 类型——统一调用风格常量（封闭六动词 + 资源域路径）、信封、设备/claim/run/事件类型（camelCase）
 * [POS]: internal/daemon 的协议词汇表。makecli 是公开 GitHub 仓库，无法 import 私有 agent-contract 模块，
 *        故在此镜像线上 JSON 形状；字段变更必须与 agent-contract 同步（先 Contract.md，后两边类型）
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package daemon

import (
	"encoding/json"
	"time"
)

// TargetHeader 是统一调用风格的 action 头。
const TargetHeader = "X-Make-Target"

// 封闭六动词中 daemon 消费的三个（Make 平台规范：新能力 = 新资源域 × 六动词）。
const (
	TargetCreateResource = "MakeService.CreateResource"
	TargetUpdateResource = "MakeService.UpdateResource"
	TargetListResources  = "MakeService.ListResources"
)

// PathPrefix 是 agent 服务段的路径前缀；设备面经 gateway 透传，内外同路径。
const PathPrefix = "/api/make/agent/v1"

// daemon 消费的资源域。
const (
	ResourceEvent           = "event"
	ResourceRunClaim        = "run-claim"
	ResourceRun             = "run"
	ResourceDevice          = "device"
	ResourceDeviceHeartbeat = "device-heartbeat"
)

// Envelope 是统一响应信封 {code, msg, data}。
type Envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data,omitempty"`
}

// ErrorData 是错误响应 data 的最小可解析形状（领域详情为兄弟字段）。
type ErrorData struct {
	Reason string `json:"reason"`
}

// Actor 是事件产生方身份。
type Actor struct {
	Kind string `json:"kind"` // end_user | agent | platform
	ID   string `json:"id,omitempty"`
}

// Block 是渠道无关内容块（daemon 只消费 text/mention 的 text 呈现）。
type Block struct {
	Kind string `json:"kind"` // text | image | file | mention
	Text string `json:"text,omitempty"`
	URL  string `json:"url,omitempty"`
	Name string `json:"name,omitempty"`
}

// Event 是事件 envelope（读取用）。
type Event struct {
	Seq       int64           `json:"seq"`
	Branch    int16           `json:"branch"`
	RunID     string          `json:"runID,omitempty"`
	Type      string          `json:"type"`
	Actor     Actor           `json:"actor"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
}

// NewEvent 是待写入事件（append 用）。
type NewEvent struct {
	Type    string          `json:"type"`
	Actor   Actor           `json:"actor"`
	Payload json.RawMessage `json:"payload,omitempty"`
	RunID   string          `json:"runID,omitempty"`
}

// UserMessagePayload 是 user_message 事件的 payload（prompt 构造用）。
type UserMessagePayload struct {
	Blocks  []Block `json:"blocks"`
	EndUser string  `json:"endUser"`
}

// DeviceCapability 是设备探测到的一个 CLI。
type DeviceCapability struct {
	Provider string `json:"provider"`
	Version  string `json:"version,omitempty"`
}

// CreateDeviceRequest / Response —— 注册幂等，身份来自 token。
type CreateDeviceRequest struct {
	Name         string             `json:"name"`
	Capabilities []DeviceCapability `json:"capabilities"`
}

type CreateDeviceResponse struct {
	DeviceID string `json:"deviceID"`
}

// CreateDeviceHeartbeatRequest / Response —— 15s 周期；actions 是平台→设备指令通道。
type CreateDeviceHeartbeatRequest struct {
	Capabilities []DeviceCapability `json:"capabilities,omitempty"`
}

type DeviceAction struct {
	Kind  string `json:"kind"` // v1 仅 cancel_run
	RunID string `json:"runID,omitempty"`
}

type CreateDeviceHeartbeatResponse struct {
	Ack     bool           `json:"ack"`
	Actions []DeviceAction `json:"actions,omitempty"`
}

// CreateRunClaimRequest —— 设备身份来自 token（gateway 注入），请求体不带 deviceID。
type CreateRunClaimRequest struct {
	Capabilities []string `json:"capabilities"`
	Max          int      `json:"max"`
}

// AgentBundle 是 claim 下发的 agent 渲染包，execenv 据此渲染工作目录。
type AgentBundle struct {
	Name         string          `json:"name"`
	Instructions string          `json:"instructions"`
	RunParams    json.RawMessage `json:"runParams,omitempty"`
}

// SeqRange 是触发事件区间 [FromSeq, ToSeq]。
type SeqRange struct {
	FromSeq int64 `json:"fromSeq"`
	ToSeq   int64 `json:"toSeq"`
}

// ResumeState 是会话连续性状态。
type ResumeState struct {
	CLISessionID string `json:"cliSessionID,omitempty"`
	WorkDir      string `json:"workDir,omitempty"`
}

// ChainState 是链的当前状态。
type ChainState struct {
	ID         string `json:"id"`
	Depth      int    `json:"depth"`
	BudgetLeft int    `json:"budgetLeft"`
}

// RunClaim 是 claim 的单条结果。
type RunClaim struct {
	RunID        string      `json:"runID"`
	SessionID    string      `json:"sessionID"`
	LeaseToken   string      `json:"leaseToken"`
	LeaseSeconds int         `json:"leaseSeconds"`
	Agent        AgentBundle `json:"agent"`
	Trigger      SeqRange    `json:"trigger"`
	Resume       ResumeState `json:"resume"`
	Chain        ChainState  `json:"chain"`
}

// Usage 是 CLI 上报的 token 计数（记录展示用，不参与计费）。
type Usage struct {
	InputTokens         int64 `json:"inputTokens"`
	OutputTokens        int64 `json:"outputTokens"`
	CacheReadTokens     int64 `json:"cacheReadTokens,omitempty"`
	CacheCreationTokens int64 `json:"cacheCreationTokens,omitempty"`
}

// run 状态机目标值与类型化失败原因（Contract.md §3.3）。
const (
	RunStatusRunning   = "running"
	RunStatusCompleted = "completed"
	RunStatusFailed    = "failed"
	RunStatusCancelled = "cancelled"

	FailReasonCLICrash = "cli_crash"
	FailReasonTimeout  = "timeout"
)

// UpdateRunRequest 是 run 状态迁移的统一请求——语义由 status 目标值表达：
// running / completed / failed 必带 lease；cancelled 带 lease 即设备收尾。
type UpdateRunRequest struct {
	RunID         string `json:"runID"`
	Status        string `json:"status"`
	LeaseToken    string `json:"leaseToken,omitempty"`
	CLISessionID  string `json:"cliSessionID,omitempty"`
	WorkDir       string `json:"workDir,omitempty"`
	Usage         *Usage `json:"usage,omitempty"`
	FailureReason string `json:"failureReason,omitempty"`
}

// CreateEventsRequest —— 租约 append：batchSeq per-run 从 1 单调递增，
// ≤ 已记录值的批次被服务端幂等吸收（模糊重试安全）。
type CreateEventsRequest struct {
	SessionID  string     `json:"sessionID"`
	LeaseToken string     `json:"leaseToken"`
	BatchSeq   int64      `json:"batchSeq"`
	Events     []NewEvent `json:"events"`
}

type CreateEventsResponse struct {
	Appended  int  `json:"appended"`
	Duplicate bool `json:"duplicate,omitempty"`
}

// ListEventsRequest —— 区间读取；from/to 是事件资源域的专属参数（seq 游标语义）。
type ListEventsRequest struct {
	SessionID string `json:"sessionID"`
	Branch    int16  `json:"branch"`
	From      int64  `json:"from"`
	To        int64  `json:"to,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}
