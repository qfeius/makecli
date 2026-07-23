// 移植自 github.com/smallnest/pigo (MIT License, Copyright (c) 2026 smallnest)，按 makecli 需要裁剪改写。
// This file implements pi's two-layer agent loop (US-006, FR-1). It strings
// together streaming assistant responses, batch tool execution, and the loop's
// six hooks with control flow kept faithful to pi's runLoop:
//
//   - Inner loop: one turn = stream an assistant response → execute its tool
//     calls → feed the results back, repeating until an assistant message has no
//     tool calls (a natural turn end).
//   - Outer loop: after the inner loop settles, pull getFollowUpMessages; if any
//     are returned they become the next pending input and the inner loop runs
//     again, otherwise the run ends.
//
// Per-turn hooks after each turn_end: getSteeringMessages (pulled after tool
// execution and injected before the next turn), prepareNextTurn (may swap
// context / model / thinkingLevel), shouldStopAfterTurn (true ⇒ agent_end +
// exit). Two stop reasons are handled specially: length (the response was
// truncated by the token cap) fails every tool call so the model resends
// (failToolCallsFromTruncatedMessage); error / aborted end the run immediately.
//
// agentLoop starts a fresh run from a prompt already appended to the context.
package loop

import (
	"context"

	"github.com/qfeius/makecli/internal/agent/core"
	"github.com/qfeius/makecli/internal/agent/tool"
)

// TurnUpdate is the optional result of PrepareNextTurn: any non-nil field
// replaces the corresponding piece of loop state before the next turn. It lets
// a caller swap the trimmed context, system prompt, tool set, model, or
// thinking level between turns (FR-6).
type TurnUpdate struct {
	Messages      *core.MessageList
	SystemPrompt  *string
	Tools         *[]core.AgentTool
	Model         *string
	ThinkingLevel *core.ThinkingLevel
}

// RunConfig is the full configuration for a loop run: the per-turn streaming
// config (embedded LoopConfig), the batch tool-execution config, and the four
// loop-level hooks. Every hook is optional (nil = default behavior).
type RunConfig struct {
	LoopConfig
	// Batch holds the tool registry and the prepare/before/after hooks used to
	// execute each assistant message's tool calls.
	Batch tool.BatchConfig

	// GetFollowUpMessages is consulted after the inner loop settles (an assistant
	// message with no tool calls). Returning messages continues the outer loop
	// with them as the next input; returning none ends the run (FR-9).
	GetFollowUpMessages func(ctx context.Context, agentCtx *core.AgentContext) []core.AgentMessage
	// GetSteeringMessages is pulled after each turn's tool execution and injected
	// before the next turn (pi per-turn semantics, FR-8).
	GetSteeringMessages func(ctx context.Context) []core.AgentMessage
	// PrepareNextTurn runs after each turn_end and may swap context / model /
	// thinkingLevel for the next turn (FR-6).
	PrepareNextTurn func(ctx context.Context, agentCtx *core.AgentContext) *TurnUpdate
	// ShouldStopAfterTurn runs after each turn_end; true ends the run with an
	// agent_end event (FR-7).
	ShouldStopAfterTurn func(ctx context.Context, agentCtx *core.AgentContext) bool

	// EventBuffer is the buffer size of the emitted EventStream. 0 gives fully
	// synchronous back-pressure (matching pi's awaited emit).
	EventBuffer int

	// SessionID, when set, is carried in the run's agent_start event so a
	// stream-json consumer sees the backing session id in the first event and can
	// resume the run later (对标 pi/Claude Code).
	SessionID string
}

// LoopEventStream is the stream returned by the loop entry points: it carries
// AgentEvents and yields the messages newly produced during the run.
type LoopEventStream = core.EventStream[core.AgentEvent, []core.AgentMessage]

// agentLoop starts a fresh run. The caller has already appended the initiating
// user message(s) to agentCtx.Messages. It returns immediately with an
// EventStream; a producer goroutine drives the loop and closes the stream when
// the run ends.
func agentLoop(ctx context.Context, agentCtx *core.AgentContext, cfg RunConfig) *LoopEventStream {
	stream := core.NewEventStream[core.AgentEvent, []core.AgentMessage](cfg.EventBuffer)
	go runLoop(ctx, agentCtx, cfg, stream)
	return stream
}

// StartRun is the exported entry point for a fresh run, used by out-of-package
// drivers (the interactive REPL, US-022). It is a thin wrapper over agentLoop so
// the loop internals stay unexported while callers outside the package can
// still launch a run and consume its event stream.
func StartRun(ctx context.Context, agentCtx *core.AgentContext, cfg RunConfig) *LoopEventStream {
	return agentLoop(ctx, agentCtx, cfg)
}

// runLoop is the producer: it drives the two-layer loop, emitting events onto
// stream and setting the stream result to the messages produced during the run.
func runLoop(ctx context.Context, agentCtx *core.AgentContext, cfg RunConfig, stream *LoopEventStream) {
	startIdx := len(agentCtx.Messages)
	// newMessages returns the messages appended since the run began.
	newMessages := func() []core.AgentMessage {
		if len(agentCtx.Messages) <= startIdx {
			return nil
		}
		out := make([]core.AgentMessage, len(agentCtx.Messages)-startIdx)
		copy(out, agentCtx.Messages[startIdx:])
		return out
	}
	emit := func(ev core.AgentEvent) error { return stream.Emit(ctx, ev) }

	// finish emits agent_end (unless suppressed by a prior emit error), records
	// the run result, and closes the stream exactly once.
	finish := func() {
		msgs := newMessages()
		_ = emit(core.AgentEndEvent{Messages: msgs})
		stream.SetResult(msgs)
		stream.Close()
	}

	if err := emit(core.AgentStartEvent{SessionID: cfg.SessionID}); err != nil {
		finish()
		return
	}

	for { // outer loop: pending / follow-up messages
		for { // inner loop: turns until no tool calls
			if err := emit(core.TurnStartEvent{}); err != nil {
				finish()
				return
			}

			assistant, err := streamAssistantResponse(ctx, agentCtx, cfg.LoopConfig, func(c context.Context, ev core.AgentEvent) error {
				return stream.Emit(c, ev)
			})
			if err != nil {
				// emit was cancelled mid-stream; end the run.
				finish()
				return
			}

			switch assistant.StopReason {
			case core.StopReasonLength:
				// Truncated by the token cap: fail every tool call so the model
				// resends, then continue feeding back.
				toolResults := failToolCallsFromTruncatedMessage(agentCtx, assistant)
				if err := emit(core.TurnEndEvent{Message: assistant, ToolResults: toolResults}); err != nil {
					finish()
					return
				}
				if afterTurn(ctx, agentCtx, &cfg, true) {
					finish()
					return
				}
				continue
			case core.StopReasonError, core.StopReasonAborted:
				// Terminal failure: emit the turn end and stop.
				_ = emit(core.TurnEndEvent{Message: assistant})
				finish()
				return
			}

			calls := toAgentToolCalls(assistant.ToolCalls())
			if len(calls) == 0 {
				// Natural turn end: no tools to run.
				if err := emit(core.TurnEndEvent{Message: assistant}); err != nil {
					finish()
					return
				}
				if afterTurn(ctx, agentCtx, &cfg, false) {
					finish()
					return
				}
				break // exit inner loop → consult follow-up messages
			}

			toolResults, allTerminate := tool.ExecuteToolCalls(ctx, cfg.Batch, calls, func(c context.Context, ev core.AgentEvent) error {
				return stream.Emit(c, ev)
			})
			for _, tr := range toolResults {
				agentCtx.Messages = append(agentCtx.Messages, tr)
			}
			if err := emit(core.TurnEndEvent{Message: assistant, ToolResults: toolResults}); err != nil {
				finish()
				return
			}
			if allTerminate {
				// Every tool asked to terminate the run.
				finish()
				return
			}
			if afterTurn(ctx, agentCtx, &cfg, true) {
				finish()
				return
			}
			// Feed the tool results back into the next turn.
		}

		// Inner loop settled: consult follow-up messages.
		if cfg.GetFollowUpMessages != nil {
			if follow := cfg.GetFollowUpMessages(ctx, agentCtx); len(follow) > 0 {
				agentCtx.Messages = append(agentCtx.Messages, follow...)
				continue // outer loop with the follow-ups as new input
			}
		}
		break
	}

	finish()
}

// afterTurn runs the per-turn hooks after a turn_end. When hadToolExecution is
// true it first pulls getSteeringMessages and injects them before the next turn
// (pi per-turn semantics). It then applies prepareNextTurn and finally consults
// shouldStopAfterTurn, returning true when the run should end.
// （pigo 原版在此处还挂有 auto-compaction 并因此接收 emit；makecli 移植裁剪了
// compaction 能力，emit 参数一并移除。）
func afterTurn(ctx context.Context, agentCtx *core.AgentContext, cfg *RunConfig, hadToolExecution bool) (stop bool) {
	if hadToolExecution && cfg.GetSteeringMessages != nil {
		if steer := cfg.GetSteeringMessages(ctx); len(steer) > 0 {
			agentCtx.Messages = append(agentCtx.Messages, steer...)
		}
	}
	if cfg.PrepareNextTurn != nil {
		if upd := cfg.PrepareNextTurn(ctx, agentCtx); upd != nil {
			applyTurnUpdate(agentCtx, cfg, upd)
		}
	}
	if cfg.ShouldStopAfterTurn != nil {
		return cfg.ShouldStopAfterTurn(ctx, agentCtx)
	}
	return false
}

// applyTurnUpdate applies a non-nil TurnUpdate to the mutable loop state: any
// set field replaces the current context / config value for the next turn.
func applyTurnUpdate(agentCtx *core.AgentContext, cfg *RunConfig, upd *TurnUpdate) {
	if upd.Messages != nil {
		agentCtx.Messages = *upd.Messages
	}
	if upd.SystemPrompt != nil {
		agentCtx.SystemPrompt = *upd.SystemPrompt
	}
	if upd.Tools != nil {
		agentCtx.Tools = *upd.Tools
	}
	if upd.Model != nil {
		cfg.Model = *upd.Model
	}
	if upd.ThinkingLevel != nil {
		cfg.ThinkingLevel = *upd.ThinkingLevel
	}
}

// failToolCallsFromTruncatedMessage produces an error tool-result message for
// every tool call in a truncated (stopReason=length) assistant message, telling
// the model the response was cut off and to resend. The results are appended to
// the context and returned. Mirrors pi's failToolCallsFromTruncatedMessage.
func failToolCallsFromTruncatedMessage(agentCtx *core.AgentContext, assistant core.AssistantMessage) []core.ToolResultMessage {
	calls := assistant.ToolCalls()
	if len(calls) == 0 {
		return nil
	}
	results := make([]core.ToolResultMessage, 0, len(calls))
	for _, c := range calls {
		results = append(results, core.ToolResultMessage{
			RoleField:  core.RoleToolResult,
			ToolCallID: c.ID,
			ToolName:   c.Name,
			Content: core.ContentList{core.NewTextContent(
				"The previous response was truncated because it hit the output token limit, " +
					"so this tool call was not executed. Please send a shorter response and retry.")},
			IsError: true,
		})
	}
	for _, r := range results {
		agentCtx.Messages = append(agentCtx.Messages, r)
	}
	return results
}

// toAgentToolCalls converts the assistant message's ToolCallContent blocks into
// the loop-level AgentToolCall view executeToolCalls consumes.
func toAgentToolCalls(blocks []core.ToolCallContent) []core.AgentToolCall {
	if len(blocks) == 0 {
		return nil
	}
	calls := make([]core.AgentToolCall, len(blocks))
	for i, b := range blocks {
		calls[i] = core.AgentToolCall{ID: b.ID, Name: b.Name, Arguments: b.Arguments}
	}
	return calls
}
