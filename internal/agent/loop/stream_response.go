// 移植自 github.com/smallnest/pigo (MIT License, Copyright (c) 2026 smallnest)，按 makecli 需要裁剪改写。
// This file implements streamAssistantResponse (US-003): it shapes the context
// into a provider request, resolves the API key dynamically, drives the
// provider stream, and back-fills the partial assistant message into the
// context while emitting message_start / message_update / message_end events.
package loop

import (
	"context"

	"github.com/qfeius/makecli/internal/agent/core"
	"github.com/qfeius/makecli/internal/agent/llm"
)

// LoopConfig holds the pluggable behavior of the agent loop. Every hook is
// optional (nil = use the default). The pointer/func-field pattern mirrors pi's
// optional callbacks.
type LoopConfig struct {
	// Model is the model id passed to StreamFn.
	Model string
	// APIKey is the static fallback key when GetAPIKey is nil or returns "".
	APIKey string
	// ThinkingLevel is the reasoning effort for requests.
	ThinkingLevel core.ThinkingLevel
	// Stream produces the provider stream. Required (defaults are wired by
	// callers/tests, e.g. a fake provider).
	Stream llm.StreamFn

	// TransformContext optionally rewrites the message list before conversion
	// (context trimming/injection). Contract: must not error; on failure return
	// a safe fallback. Runs first.
	TransformContext func(ctx context.Context, msgs core.MessageList) core.MessageList
	// ConvertToLlm optionally filters UI-only messages. Defaults to identity.
	// Contract: must not error.
	ConvertToLlm func(msgs core.MessageList) core.MessageList
	// GetAPIKey optionally resolves a fresh key per request (handles short-lived
	// token expiry). Falls back to APIKey when nil or empty.
	GetAPIKey func(ctx context.Context, provider string) string
	// Provider is the provider name passed to GetAPIKey.
	Provider string

	// （pigo 原版此处还有 ContextWindow/Compaction/SummaryStream/SummaryModel 等
	// auto-compaction 配置；makecli 移植裁剪了 compaction 能力。）

	// Extra is forwarded to StreamConfig.Extra.
	Extra map[string]any
}

// streamAssistantResponse runs one assistant turn: it builds the request from
// agentCtx, streams the provider response, back-fills the partial into
// agentCtx.Messages, and returns the final assistant message. The sequence
// (transformContext → convertToLlm → resolve key → stream → drain) is kept
// identical to pi. It never returns an error for a request failure — such
// failures arrive as a terminal assistant message with stopReason error/aborted.
func streamAssistantResponse(ctx context.Context, agentCtx *core.AgentContext, cfg LoopConfig, emit core.EmitFunc) (core.AssistantMessage, error) {
	// 1. transformContext (optional, must not error).
	msgs := agentCtx.Messages
	if cfg.TransformContext != nil {
		msgs = cfg.TransformContext(ctx, msgs)
	}
	// 2. convertToLlm (filter UI-only; default identity).
	if cfg.ConvertToLlm != nil {
		msgs = cfg.ConvertToLlm(msgs)
	}
	// 3. shape the LLM context.（局部变量名 llmContext 避免遮蔽 llm 包名）
	llmContext := llm.LlmContext{
		SystemPrompt: agentCtx.SystemPrompt,
		Messages:     msgs,
		Tools:        agentCtx.Tools,
	}
	// 4. resolve API key dynamically, fall back to static.
	key := cfg.APIKey
	if cfg.GetAPIKey != nil {
		if dyn := cfg.GetAPIKey(ctx, cfg.Provider); dyn != "" {
			key = dyn
		}
	}
	// 5. build the provider stream.
	stream, err := cfg.Stream(ctx, cfg.Model, llmContext, llm.StreamConfig{
		APIKey:        key,
		ThinkingLevel: cfg.ThinkingLevel,
		Extra:         cfg.Extra,
	})
	if err != nil {
		// Early "cannot build stream" failure: synthesize a terminal message so
		// the loop has a uniform assistant message to record.
		return newErrorAssistantMessage(cfg, err), nil
	}

	// 6. drain the stream, back-filling the partial into the context.
	addedPartial := false
	backfill := func(partial core.AssistantMessage) {
		if !addedPartial {
			agentCtx.Messages = append(agentCtx.Messages, partial)
			addedPartial = true
		} else {
			agentCtx.Messages[len(agentCtx.Messages)-1] = partial
		}
	}

	for ev := range stream.Events() {
		switch e := ev.(type) {
		case llm.StreamStartEvent:
			backfill(e.Partial)
			if err := emit(ctx, core.MessageStartEvent{Message: e.Partial}); err != nil {
				return core.AssistantMessage{}, err
			}
		case llm.StreamTextEvent:
			backfill(e.Partial)
			if err := emit(ctx, core.MessageUpdateEvent{Message: e.Partial, AssistantMessageEvent: e}); err != nil {
				return core.AssistantMessage{}, err
			}
		case llm.StreamThinkingEvent:
			backfill(e.Partial)
			if err := emit(ctx, core.MessageUpdateEvent{Message: e.Partial, AssistantMessageEvent: e}); err != nil {
				return core.AssistantMessage{}, err
			}
		case llm.StreamToolCallEvent:
			backfill(e.Partial)
			if err := emit(ctx, core.MessageUpdateEvent{Message: e.Partial, AssistantMessageEvent: e}); err != nil {
				return core.AssistantMessage{}, err
			}
		case llm.StreamDoneEvent:
			finalizeMessage(agentCtx, e.Message, &addedPartial)
			if err := emit(ctx, core.MessageEndEvent{Message: e.Message}); err != nil {
				return core.AssistantMessage{}, err
			}
			return e.Message, nil
		case llm.StreamErrorEvent:
			finalizeMessage(agentCtx, e.Message, &addedPartial)
			if err := emit(ctx, core.MessageEndEvent{Message: e.Message}); err != nil {
				return core.AssistantMessage{}, err
			}
			return e.Message, nil
		}
	}

	// 7. stream ended without done/error: fall back to the stream result.
	final, resErr := stream.Result(ctx)
	if resErr != nil {
		return newErrorAssistantMessage(cfg, resErr), nil
	}
	finalizeMessage(agentCtx, final, &addedPartial)
	if err := emit(ctx, core.MessageEndEvent{Message: final}); err != nil {
		return core.AssistantMessage{}, err
	}
	return final, nil
}

// finalizeMessage replaces the placeholder partial with the final message, or
// appends it if the provider sent done/error without a prior start.
func finalizeMessage(agentCtx *core.AgentContext, final core.AssistantMessage, addedPartial *bool) {
	if *addedPartial {
		agentCtx.Messages[len(agentCtx.Messages)-1] = final
	} else {
		agentCtx.Messages = append(agentCtx.Messages, final)
		*addedPartial = true
	}
}

// newErrorAssistantMessage builds a terminal assistant message for an early
// failure that never produced a provider stream.
func newErrorAssistantMessage(cfg LoopConfig, err error) core.AssistantMessage {
	return core.AssistantMessage{
		RoleField:    core.RoleAssistant,
		Model:        cfg.Model,
		Provider:     cfg.Provider,
		StopReason:   core.StopReasonError,
		ErrorMessage: err.Error(),
	}
}
