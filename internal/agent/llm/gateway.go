/**
 * [INPUT]: 依赖 bufio、bytes、context、encoding/json、errors、fmt、io、net/http、sort、strings、time、internal/agent/core
 * [OUTPUT]: 对外提供 GatewayProvider（llm.Provider 实现：OpenAI 兼容 SSE 指向 gateway /v1/chat/completions），
 *           经 StreamFnFromProvider 即得 loop 可用的 StreamFn
 * [POS]: internal/agent/llm 的唯一具体 Provider——keyless code agent 的模型面：平台 token 只开模型门，
 *        model 填平台别名；请求编码/SSE 解码移植自 pigo internal/provider 的 providers.go/openai.go
 *        (MIT License, Copyright (c) 2026 smallnest)，剥离多模态 image 分支与看门狗/重试传输，
 *        按 internal/agent/client.go 的简洁风格重写（bufio 逐行 + data: 前缀 + OpenAI 风格错误体）
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/qfeius/makecli/internal/agent/core"
)

// gatewayProviderName 是 Provider.Name 与 AssistantMessage.Provider 的标识。
const gatewayProviderName = "gateway"

// GatewayProvider implements llm.Provider against the makecli gateway's
// OpenAI-compatible /v1/chat/completions endpoint. 鉴权与头部同 client.go：
// Authorization: Bearer <token> + X-Session-ID（服务端计量维度）。
//
// Failure contract (pigo FR-13): anything before the stream is established —
// request encoding, transport failure, a non-200 status — is a returned error;
// once streaming has begun every failure rides the stream as a terminal
// StreamErrorEvent (stopReason=error/aborted) and is never a Go error.
type GatewayProvider struct {
	gatewayURL string
	token      string
	sessionID  string
	http       *http.Client
}

// NewGatewayProvider 构造指向 gateway 的 Provider。token 是平台 token（缺省
// 兜底；每请求的 StreamConfig.APIKey 非空时覆盖），sessionID 进 X-Session-ID。
func NewGatewayProvider(gatewayURL, token, sessionID string) *GatewayProvider {
	return &GatewayProvider{
		gatewayURL: strings.TrimSuffix(gatewayURL, "/"),
		token:      token,
		sessionID:  sessionID,
		// 流式响应无总超时；连接与首字节交给服务端与 ctx 兜底（同 client.go）。
		http: &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: 120 * time.Second}},
	}
}

// Name implements Provider.
func (p *GatewayProvider) Name() string { return gatewayProviderName }

// Models implements Provider. 模型别名由平台侧解析，本地无目录可举。
func (p *GatewayProvider) Models() []Model { return nil }

// StreamCompletion implements Provider: it POSTs the OpenAI-compatible request
// and drives the SSE decode on a producer goroutine.
func (p *GatewayProvider) StreamCompletion(ctx context.Context, req CompletionRequest) (*AssistantMessageEventStream, error) {
	body, err := encodeGatewayRequest(req)
	if err != nil {
		return nil, fmt.Errorf("gateway: build request body: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.gatewayURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gateway: build request: %w", err)
	}
	token := req.Config.APIKey
	if token == "" {
		token = p.token
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("X-Session-ID", p.sessionID)

	response, err := p.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("gateway unreachable: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		defer func() { _ = response.Body.Close() }()
		return nil, decodeGatewayAPIError(response)
	}

	stream := NewAssistantMessageEventStream(0)
	go p.drainSSE(ctx, response.Body, req.Model, stream)
	return stream, nil
}

// drainSSE is the producer goroutine: it reads the SSE body line by line,
// feeds each data payload through the decoder, and finishes the stream with a
// terminal done/error event. 传输失败在此全部化为流上事件，绝不 panic。
func (p *GatewayProvider) drainSSE(ctx context.Context, body io.ReadCloser, model string, stream *AssistantMessageEventStream) {
	defer func() { _ = body.Close() }()
	dec := newGatewayDecoder(model)

	emit := func(ev AssistantMessageEvent) bool {
		if err := stream.Emit(ctx, ev); err != nil {
			// 消费方取消：记录取消原因并收尾，不再继续读流。
			stream.SetError(err)
			stream.Close()
			return false
		}
		return true
	}
	finishError := func(err error) {
		emit(dec.finishError(ctx, err))
		stream.Close()
	}
	finishDone := func() {
		if ev, ok := dec.finishDone(); ok {
			emit(ev)
		}
		stream.Close()
	}

	if !emit(StreamStartEvent{Partial: dec.partial()}) {
		return
	}

	reader := bufio.NewReader(body)
	for {
		line, err := reader.ReadString('\n')
		if payload, ok := strings.CutPrefix(strings.TrimSpace(line), "data:"); ok {
			payload = strings.TrimSpace(payload)
			if payload == "[DONE]" {
				finishDone()
				return
			}
			events, decErr := dec.decode([]byte(payload))
			if decErr != nil {
				finishError(decErr)
				return
			}
			for _, ev := range events {
				if !emit(ev) {
					return
				}
			}
		}
		if err == io.EOF {
			// [DONE] 缺失：EOF 一样收尾，已积累的部分响应不丢。
			finishDone()
			return
		}
		if err != nil {
			finishError(fmt.Errorf("read stream: %w", err))
			return
		}
	}
}

// decodeGatewayAPIError 解 OpenAI 风格错误体（同 client.go decodeAPIError 逻辑；
// client.go 保留不动故此处独立实现），解不出时保底带状态码与原文摘要。
func decodeGatewayAPIError(response *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 64<<10))
	var payload struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Error.Message != "" {
		return fmt.Errorf("gateway %d %s: %s", response.StatusCode, payload.Error.Type, payload.Error.Message)
	}
	summary := strings.TrimSpace(string(body))
	if len(summary) > 200 {
		summary = summary[:200]
	}
	return fmt.Errorf("gateway %d: %s", response.StatusCode, summary)
}

// ---------------------------------------------------------------------------
// 请求编码（移植自 pigo providers.go 的 encodeOpenAIRequest 族，剥 image 分支）。
// ---------------------------------------------------------------------------

// encodeGatewayRequest serializes a CompletionRequest into an OpenAI Chat
// Completions JSON body with streaming enabled and usage requested.
func encodeGatewayRequest(req CompletionRequest) ([]byte, error) {
	msgs := make([]map[string]any, 0, len(req.Context.Messages)+1)
	if sp := req.Context.SystemPrompt; sp != "" {
		msgs = append(msgs, map[string]any{"role": "system", "content": sp})
	}
	for _, m := range req.Context.Messages {
		msgs = append(msgs, encodeGatewayMessage(m)...)
	}
	body := map[string]any{
		"model":          req.Model,
		"messages":       msgs,
		"stream":         true,
		"stream_options": map[string]any{"include_usage": true},
	}
	if tools := encodeGatewayTools(req.Context.Tools); len(tools) > 0 {
		body["tools"] = tools
	}
	return json.Marshal(body)
}

// encodeGatewayMessage maps one core message onto the OpenAI wire shape. An
// assistant message expands to content + tool_calls in a single entry; a tool
// result becomes a role:"tool" entry keyed by tool_call_id. makecli v1 无图，
// 多模态 image 分支不移植，user 内容一律折叠为纯文本。
func encodeGatewayMessage(m core.Message) []map[string]any {
	switch msg := m.(type) {
	case core.UserMessage:
		return []map[string]any{{"role": "user", "content": core.ContentToText(msg.Content)}}
	case core.CompactionMessage:
		// A compaction checkpoint stands in for compacted history as user text.
		u := msg.AsUserMessage()
		return []map[string]any{{"role": "user", "content": core.ContentToText(u.Content)}}
	case core.AssistantMessage:
		entry := map[string]any{"role": "assistant", "content": core.ContentToText(msg.Content)}
		var toolCalls []map[string]any
		for _, c := range msg.Content {
			if tc, ok := c.(core.ToolCallContent); ok {
				toolCalls = append(toolCalls, map[string]any{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]any{
						"name":      tc.Name,
						"arguments": string(tc.Arguments),
					},
				})
			}
		}
		if len(toolCalls) > 0 {
			entry["tool_calls"] = toolCalls
		}
		return []map[string]any{entry}
	case core.ToolResultMessage:
		return []map[string]any{{
			"role":         "tool",
			"tool_call_id": msg.ToolCallID,
			"content":      core.ContentToText(msg.Content),
		}}
	default:
		return nil
	}
}

// encodeGatewayTools maps AgentTools onto the OpenAI function-tool schema.
func encodeGatewayTools(tools []core.AgentTool) []map[string]any {
	if len(tools) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(tools))
	for _, t := range tools {
		params := json.RawMessage(t.Schema())
		if len(params) == 0 {
			params = json.RawMessage("{}")
		}
		out = append(out, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        t.Name(),
				"description": t.Description(),
				"parameters":  params,
			},
		})
	}
	return out
}

// ---------------------------------------------------------------------------
// SSE 解码（移植自 pigo openai.go 的 OpenAIDecoder，收敛为 gateway 单用途）。
// ---------------------------------------------------------------------------

// gatewayToolCall accumulates one streamed tool call, keyed by its delta index.
// id/name arrive once (usually in the first fragment); arguments accumulate.
type gatewayToolCall struct {
	id   string
	name string
	args strings.Builder
}

// gatewayDecoder is the stateful SSE decoder for the gateway's OpenAI-
// compatible chunk stream. Not safe for concurrent use — drainSSE drives it
// from one goroutine.
type gatewayDecoder struct {
	model     string
	text      strings.Builder
	toolCalls map[int]*gatewayToolCall
	toolOrder []int // tool-call indices in first-seen order

	responseID    string
	responseModel string
	inputTokens   int
	outputTokens  int
	stopReason    string // mapped core stop reason (empty until finish_reason)
	done          bool
}

// newGatewayDecoder builds a fresh decoder for one streamed response. model
// 是请求所用的平台别名，回填进每个 partial 的 Model 字段。
func newGatewayDecoder(model string) *gatewayDecoder {
	return &gatewayDecoder{model: model, toolCalls: make(map[int]*gatewayToolCall)}
}

// gatewayChunk is the streamed chat.completion.chunk envelope.
type gatewayChunk struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content   string             `json:"content"`
			ToolCalls []gatewayToolDelta `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	// Some gateways surface an error object inline on the stream.
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

type gatewayToolDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// decode turns one SSE data payload into zero or more stream events.
func (d *gatewayDecoder) decode(payload []byte) ([]AssistantMessageEvent, error) {
	var chunk gatewayChunk
	if err := json.Unmarshal(payload, &chunk); err != nil {
		return nil, fmt.Errorf("gateway: parse chunk: %w", err)
	}
	if chunk.Error != nil {
		msg := "gateway stream error"
		if chunk.Error.Type != "" {
			msg = "gateway " + chunk.Error.Type
		}
		if chunk.Error.Message != "" {
			msg += ": " + chunk.Error.Message
		}
		return nil, errors.New(msg)
	}

	if chunk.ID != "" {
		d.responseID = chunk.ID
	}
	if chunk.Model != "" {
		d.responseModel = chunk.Model
	}
	if chunk.Usage != nil {
		d.inputTokens = chunk.Usage.PromptTokens
		d.outputTokens = chunk.Usage.CompletionTokens
	}

	var events []AssistantMessageEvent
	for _, choice := range chunk.Choices {
		if choice.Delta.Content != "" {
			d.text.WriteString(choice.Delta.Content)
			events = append(events, StreamTextEvent{Partial: d.partial()})
		}
		for _, tc := range choice.Delta.ToolCalls {
			d.applyToolDelta(tc)
			events = append(events, StreamToolCallEvent{Partial: d.partial()})
		}
		if choice.FinishReason != "" {
			d.stopReason = mapGatewayFinishReason(choice.FinishReason)
		}
	}
	return events, nil
}

// applyToolDelta merges one tool-call fragment into the accumulated state.
func (d *gatewayDecoder) applyToolDelta(tc gatewayToolDelta) {
	call := d.toolCalls[tc.Index]
	if call == nil {
		call = &gatewayToolCall{}
		d.toolCalls[tc.Index] = call
		d.toolOrder = append(d.toolOrder, tc.Index)
	}
	if tc.ID != "" {
		call.id = tc.ID
	}
	if tc.Function.Name != "" {
		call.name = tc.Function.Name
	}
	call.args.WriteString(tc.Function.Arguments)
}

// finishDone builds the terminal done event exactly once; the second return is
// false when the decoder already finished.
func (d *gatewayDecoder) finishDone() (AssistantMessageEvent, bool) {
	if d.done {
		return nil, false
	}
	d.done = true
	msg := d.partial()
	if msg.StopReason == "" {
		msg.StopReason = core.StopReasonEndTurn
	}
	return StreamDoneEvent{Message: msg}, true
}

// finishError builds the terminal error event: the accumulated partial with
// stopReason=error（ctx 已取消时为 aborted）+ errorMessage（FR-13 语义）。
func (d *gatewayDecoder) finishError(ctx context.Context, err error) AssistantMessageEvent {
	d.done = true
	msg := d.partial()
	msg.StopReason = core.StopReasonError
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		msg.StopReason = core.StopReasonAborted
	}
	msg.ErrorMessage = err.Error()
	return StreamErrorEvent{Message: msg, Err: err}
}

// partial materializes the accumulated state into an AssistantMessage: the text
// block first (if any), then tool-call blocks in index order.
func (d *gatewayDecoder) partial() core.AssistantMessage {
	msg := core.AssistantMessage{
		RoleField:     core.RoleAssistant,
		API:           "openai",
		Provider:      gatewayProviderName,
		Model:         d.model,
		StopReason:    d.stopReason,
		ResponseID:    d.responseID,
		ResponseModel: d.responseModel,
	}
	if d.inputTokens != 0 || d.outputTokens != 0 {
		msg.Usage = &core.Usage{InputTokens: d.inputTokens, OutputTokens: d.outputTokens}
	}
	if d.text.Len() > 0 {
		msg.Content = append(msg.Content, core.NewTextContent(d.text.String()))
	}

	idx := make([]int, len(d.toolOrder))
	copy(idx, d.toolOrder)
	sort.Ints(idx)
	for _, i := range idx {
		call := d.toolCalls[i]
		if call == nil {
			continue
		}
		args := json.RawMessage(strings.TrimSpace(call.args.String()))
		if len(args) == 0 {
			args = json.RawMessage("{}")
		}
		msg.Content = append(msg.Content, core.NewToolCallContent(call.id, call.name, args))
	}
	return msg
}

// mapGatewayFinishReason maps an OpenAI finish_reason to the core StopReason
// set. Unknown reasons default to end_turn (a natural, non-error stop).
func mapGatewayFinishReason(reason string) string {
	switch reason {
	case "length":
		return core.StopReasonLength
	case "tool_calls", "function_call":
		return core.StopReasonToolUse
	case "stop":
		return core.StopReasonEndTurn
	default:
		return core.StopReasonEndTurn
	}
}
