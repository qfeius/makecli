/**
 * [INPUT]: 依赖 encoding/json、time；线上形状真相源是 agent-design/Contract.md（黄金测试锁在 agent-contract 仓库）
 * [OUTPUT]: 对外提供 daemon 协议的 wire 类型——响应信封、设备注册/心跳、run claim 与生命周期、事件 append/list
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

// gateway 设备面允许的 target 常量（gateway API.md §1 白名单的客户端侧词汇）。
const (
	TargetRegisterDevice  = "ContextService.RegisterDevice"
	TargetHeartbeatDevice = "ContextService.HeartbeatDevice"
	TargetClaimRuns       = "ContextService.ClaimRuns"
	TargetStartRun        = "ContextService.StartRun"
	TargetCompleteRun     = "ContextService.CompleteRun"
	TargetFailRun         = "ContextService.FailRun"
	TargetAppendEvents    = "ContextService.AppendEvents"
	TargetListEvents      = "ContextService.ListEvents"
)

// Envelope 是统一响应信封 {code, msg, data}。
type Envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data,omitempty"`
}

// ErrorData 是错误响应的 data 负载。
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
	RunID     string          `json:"run_id,omitempty"`
	Type      string          `json:"type"`
	Actor     Actor           `json:"actor"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// NewEvent 是待写入事件（append 用）。
type NewEvent struct {
	Type    string          `json:"type"`
	Actor   Actor           `json:"actor"`
	Payload json.RawMessage `json:"payload,omitempty"`
	RunID   string          `json:"run_id,omitempty"`
}

// UserMessagePayload 是 user_message 事件的 payload（prompt 构造用）。
type UserMessagePayload struct {
	Blocks  []Block `json:"blocks"`
	EndUser string  `json:"end_user"`
}

// DeviceCapability 是设备探测到的一个 CLI。
type DeviceCapability struct {
	Provider string `json:"provider"`
	Version  string `json:"version,omitempty"`
}

// RegisterDeviceRequest / Response —— 注册幂等，身份来自 token。
type RegisterDeviceRequest struct {
	Name         string             `json:"name"`
	Capabilities []DeviceCapability `json:"capabilities"`
}

type RegisterDeviceResponse struct {
	DeviceID string `json:"device_id"`
}

// HeartbeatRequest / Response —— 15s 周期；actions 是平台→设备指令通道。
type HeartbeatRequest struct {
	Capabilities []DeviceCapability `json:"capabilities,omitempty"`
}

type DeviceAction struct {
	Kind  string `json:"kind"` // v1 仅 cancel_run
	RunID string `json:"run_id,omitempty"`
}

type HeartbeatResponse struct {
	Ack     bool           `json:"ack"`
	Actions []DeviceAction `json:"actions,omitempty"`
}

// ClaimRequest —— 设备身份来自 token（gateway 注入），请求体不带 device_id。
type ClaimRequest struct {
	Capabilities []string `json:"capabilities"`
	Max          int      `json:"max"`
}

// AgentBundle 是 claim 下发的 agent 渲染包，execenv 据此渲染工作目录。
type AgentBundle struct {
	Name         string          `json:"name"`
	Instructions string          `json:"instructions"`
	RunParams    json.RawMessage `json:"run_params,omitempty"`
}

// SeqRange 是触发事件区间 [FromSeq, ToSeq]。
type SeqRange struct {
	FromSeq int64 `json:"from_seq"`
	ToSeq   int64 `json:"to_seq"`
}

// ResumeState 是会话连续性状态。
type ResumeState struct {
	CLISessionID string `json:"cli_session_id,omitempty"`
	WorkDir      string `json:"work_dir,omitempty"`
}

// ChainState 是链的当前状态。
type ChainState struct {
	ID         string `json:"id"`
	Depth      int    `json:"depth"`
	BudgetLeft int    `json:"budget_left"`
}

// RunClaim 是 claim 的单条结果。
type RunClaim struct {
	RunID           string      `json:"run_id"`
	SessionID       string      `json:"session_id"`
	LeaseToken      string      `json:"lease_token"`
	LeaseTTLSeconds int         `json:"lease_ttl_s"`
	Agent           AgentBundle `json:"agent"`
	Trigger         SeqRange    `json:"trigger"`
	Resume          ResumeState `json:"resume"`
	Chain           ChainState  `json:"chain"`
}

// Usage 是 CLI 上报的 token 计数（记录展示用，不参与计费）。
type Usage struct {
	InputTokens         int64 `json:"input_tokens"`
	OutputTokens        int64 `json:"output_tokens"`
	CacheReadTokens     int64 `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int64 `json:"cache_creation_tokens,omitempty"`
}

// StartRunRequest：dispatched → running。
type StartRunRequest struct {
	RunID      string `json:"run_id"`
	LeaseToken string `json:"lease_token"`
}

// CompleteRunRequest：running → completed，回写会话连续性。
type CompleteRunRequest struct {
	RunID        string `json:"run_id"`
	LeaseToken   string `json:"lease_token"`
	CLISessionID string `json:"cli_session_id,omitempty"`
	WorkDir      string `json:"work_dir,omitempty"`
	Usage        *Usage `json:"usage,omitempty"`
}

// 类型化失败原因（Contract.md §3.3）。
const (
	FailReasonCLICrash  = "cli_crash"
	FailReasonTimeout   = "timeout"
	FailReasonCancelled = "cancelled"
)

// FailRunRequest：→ failed（reason=cancelled 时终态 cancelled）。
type FailRunRequest struct {
	RunID      string `json:"run_id"`
	LeaseToken string `json:"lease_token"`
	Reason     string `json:"reason"`
}

// AppendEventsRequest —— 租约 append：batch_seq per-run 从 1 单调递增，
// ≤ 已记录值的批次被服务端幂等吸收（模糊重试安全）。
type AppendEventsRequest struct {
	SessionID  string     `json:"session_id"`
	LeaseToken string     `json:"lease_token"`
	BatchSeq   int64      `json:"batch_seq"`
	Events     []NewEvent `json:"events"`
}

type AppendEventsResponse struct {
	Appended  int   `json:"appended"`
	NextSeq   int64 `json:"next_seq"`
	Duplicate bool  `json:"duplicate,omitempty"`
}

// ListEventsRequest —— 区间读取，恢复现场与触发区间读取共用。
type ListEventsRequest struct {
	SessionID string `json:"session_id"`
	Branch    int16  `json:"branch"`
	From      int64  `json:"from"`
	To        int64  `json:"to,omitempty"`
	Limit     int    `json:"limit,omitempty"`
}

type ListEventsResponse struct {
	Events  []Event `json:"events"`
	NextSeq int64   `json:"next_seq"`
}
