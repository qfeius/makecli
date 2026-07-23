// 移植自 github.com/smallnest/pigo (MIT License, Copyright (c) 2026 smallnest)，按 makecli 需要裁剪改写。
// Package llm holds the LLM streaming abstraction the agent loop depends on:
// the StreamFn contract, the per-delta AssistantMessageEvent set, the
// AssistantMessageEventStream (a specialization of core.EventStream), and the
// provider-agnostic Provider/Model types. 网关适配器（实现 StreamFn，指向
// makecli gateway）由后续任务在本包落地；本文件只含纯类型，零外部依赖。
//
// 类型取自 pigo 的 internal/provider/provider.go 与 provider_interface.go 中
// 循环所需的最小集合；pigo 的 transport/openai/auth/registry/presets 等具体
// 厂商实现不移植。
//
// Contract (对齐 pigo FR-13): a StreamFn never expresses a request failure by
// returning an error. Runtime failures are encoded as an error event plus a
// terminal assistant message (stopReason=error/aborted + errorMessage). The
// returned error is reserved for the earliest "could not even build the
// stream" case.
package llm

import (
	"context"

	"github.com/qfeius/makecli/internal/agent/core"
)

// AssistantMessageEvent is the sealed interface for provider stream deltas. The
// loop dispatches on EventKind; the raw event is also surfaced to consumers via
// MessageUpdateEvent.AssistantMessageEvent.
type AssistantMessageEvent interface {
	isAssistantMessageEvent()
	// EventKind returns the delta discriminant.
	EventKind() string
}

// AssistantMessageEvent kinds.
const (
	StreamEventStart    = "start"
	StreamEventText     = "text"
	StreamEventThinking = "thinking"
	StreamEventToolCall = "toolcall"
	StreamEventDone     = "done"
	StreamEventError    = "error"
)

// StreamStartEvent carries the initial (usually empty) partial message.
type StreamStartEvent struct{ Partial core.AssistantMessage }

// StreamTextEvent carries the partial message after a text delta.
type StreamTextEvent struct{ Partial core.AssistantMessage }

// StreamThinkingEvent carries the partial after a thinking delta.
type StreamThinkingEvent struct{ Partial core.AssistantMessage }

// StreamToolCallEvent carries the partial after a tool-call delta.
type StreamToolCallEvent struct{ Partial core.AssistantMessage }

// StreamDoneEvent is the terminal success event; Message is the final response.
type StreamDoneEvent struct{ Message core.AssistantMessage }

// StreamErrorEvent is the terminal failure event; Message carries the terminal
// assistant message (stopReason=error/aborted + errorMessage).
type StreamErrorEvent struct {
	Message core.AssistantMessage
	Err     error
}

func (StreamStartEvent) isAssistantMessageEvent()    {}
func (StreamTextEvent) isAssistantMessageEvent()     {}
func (StreamThinkingEvent) isAssistantMessageEvent() {}
func (StreamToolCallEvent) isAssistantMessageEvent() {}
func (StreamDoneEvent) isAssistantMessageEvent()     {}
func (StreamErrorEvent) isAssistantMessageEvent()    {}

func (StreamStartEvent) EventKind() string    { return StreamEventStart }
func (StreamTextEvent) EventKind() string     { return StreamEventText }
func (StreamThinkingEvent) EventKind() string { return StreamEventThinking }
func (StreamToolCallEvent) EventKind() string { return StreamEventToolCall }
func (StreamDoneEvent) EventKind() string     { return StreamEventDone }
func (StreamErrorEvent) EventKind() string    { return StreamEventError }

// AssistantMessageEventStream is the provider-level stream: deltas of type
// AssistantMessageEvent with a final AssistantMessage result. isComplete fires
// on done/error; extractResult takes the terminal event's message.
type AssistantMessageEventStream = core.EventStream[AssistantMessageEvent, core.AssistantMessage]

// NewAssistantMessageEventStream builds a provider stream wired with the
// done/error completion callbacks.
func NewAssistantMessageEventStream(buffer int) *AssistantMessageEventStream {
	s := core.NewEventStream[AssistantMessageEvent, core.AssistantMessage](buffer)
	s.IsComplete = func(e AssistantMessageEvent) bool {
		k := e.EventKind()
		return k == StreamEventDone || k == StreamEventError
	}
	s.ExtractResult = func(e AssistantMessageEvent) core.AssistantMessage {
		switch ev := e.(type) {
		case StreamDoneEvent:
			return ev.Message
		case StreamErrorEvent:
			return ev.Message
		default:
			return core.AssistantMessage{}
		}
	}
	return s
}

// LlmContext is the shaped request handed to a StreamFn: the system prompt, the
// LLM-bound messages (UI-only messages already filtered), and the tools.
type LlmContext struct {
	SystemPrompt string
	Messages     core.MessageList
	Tools        []core.AgentTool
}

// StreamConfig carries per-request settings for a StreamFn.
type StreamConfig struct {
	APIKey        string
	ThinkingLevel core.ThinkingLevel
	// Extra holds provider-specific options; opaque to the loop.
	Extra map[string]any
}

// StreamFn produces a provider stream for a model + shaped context. Per the
// contract it returns an error only for early "cannot build the stream"
// failures; all runtime failures ride the returned stream as error events.
type StreamFn func(ctx context.Context, model string, llm LlmContext, cfg StreamConfig) (*AssistantMessageEventStream, error)

// Model is provider-agnostic metadata describing a single model's identity and
// capabilities. Providers construct these; the loop/UI consume them.
type Model struct {
	// Provider is the provider name (e.g. "anthropic", "openai").
	Provider string `json:"provider"`
	// ID is the provider-specific model id.
	ID string `json:"id"`
	// DisplayName is a human-friendly label; falls back to ID when empty.
	DisplayName string `json:"displayName,omitempty"`
	// ContextWindow is the maximum input+output token window, 0 if unknown.
	ContextWindow int `json:"contextWindow,omitempty"`
	// MaxOutputTokens is the max tokens the model may emit per response, 0 if
	// unknown.
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
	// SupportsThinking reports whether the model exposes a reasoning/thinking
	// channel.
	SupportsThinking bool `json:"supportsThinking,omitempty"`
	// SupportsTools reports whether the model can call tools.
	SupportsTools bool `json:"supportsTools,omitempty"`
	// SupportsImages reports whether the model accepts image (multimodal) input.
	// When false, an image block in the request is reported as a hard error
	// rather than silently dropped, so the user learns the model cannot see it.
	SupportsImages bool `json:"supportsImages,omitempty"`
	// ThinkingLevels maps unified thinking levels to this model's wire values.
	// nil when the model does not support thinking.
	ThinkingLevels core.ThinkingLevelMap `json:"-"`
}

// CompletionRequest is the provider-agnostic input to StreamCompletion: the
// model id, the shaped LLM context, and per-request options.
type CompletionRequest struct {
	// Model is the provider-specific model id to complete against.
	Model string
	// Context is the shaped request (system prompt, LLM-bound messages, tools).
	Context LlmContext
	// Config carries per-request options (API key, thinking level, extras).
	Config StreamConfig
}

// Provider is the unified streaming interface implemented by every backend. It
// hides per-vendor differences behind a single AssistantMessageEvent stream.
// Failures follow the same contract as StreamFn:
//
//   - "cannot even build the stream" (bad config, missing model) → returned error.
//   - any runtime failure once streaming has begun → a terminal StreamErrorEvent
//     carrying an assistant message with stopReason=error/aborted, after which
//     the stream is closed. It is never a returned error.
type Provider interface {
	// Name returns the provider's identifier (matches Model.Provider).
	Name() string
	// Models lists the models this provider can serve.
	Models() []Model
	// StreamCompletion streams a completion for req. Per the dual failure model
	// it returns an error only for the earliest "cannot build the stream" case;
	// all runtime failures ride the returned stream as a terminal error event.
	StreamCompletion(ctx context.Context, req CompletionRequest) (*AssistantMessageEventStream, error)
}

// StreamFnFromProvider adapts a Provider to the loop's StreamFn contract so a
// Provider can drive streamAssistantResponse directly. The two failure models
// are identical, so the adaptation is a straight delegation.
func StreamFnFromProvider(p Provider) StreamFn {
	return func(ctx context.Context, model string, llm LlmContext, cfg StreamConfig) (*AssistantMessageEventStream, error) {
		return p.StreamCompletion(ctx, CompletionRequest{Model: model, Context: llm, Config: cfg})
	}
}
