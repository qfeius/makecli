// 移植自 github.com/smallnest/pigo (MIT License, Copyright (c) 2026 smallnest)，按 makecli 需要裁剪改写。
package loop

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/qfeius/makecli/internal/agent/core"
	"github.com/qfeius/makecli/internal/agent/llm"
	"github.com/qfeius/makecli/internal/agent/tool"
)

// collectStream drains a LoopEventStream, returning the event types in order
// and the run result messages.
func collectStream(t *testing.T, s *LoopEventStream) ([]string, []core.AgentMessage) {
	t.Helper()
	var kinds []string
	for ev := range s.Events() {
		kinds = append(kinds, ev.EventType())
	}
	msgs, err := s.Result(context.Background())
	if err != nil {
		t.Fatalf("stream result: %v", err)
	}
	return kinds, msgs
}

// oneToolAssistant builds an assistant message with a single tool call.
func oneToolAssistant(id, name string) core.AssistantMessage {
	return core.AssistantMessage{
		RoleField:  core.RoleAssistant,
		StopReason: core.StopReasonToolUse,
		Content:    core.ContentList{core.NewToolCallContent(id, name, json.RawMessage(`{}`))},
	}
}

// scriptedStream returns a StreamFn that emits one StreamDoneEvent per call,
// consuming msgs in order. Extra calls beyond msgs emit a plain end_turn.
func scriptedStream(msgs []core.AssistantMessage) llm.StreamFn {
	i := 0
	return func(ctx context.Context, model string, llmContext llm.LlmContext, cfg llm.StreamConfig) (*llm.AssistantMessageEventStream, error) {
		var msg core.AssistantMessage
		if i < len(msgs) {
			msg = msgs[i]
		} else {
			msg = core.AssistantMessage{RoleField: core.RoleAssistant, StopReason: core.StopReasonEndTurn}
		}
		i++
		s := llm.NewAssistantMessageEventStream(0)
		go func() {
			_ = s.Emit(ctx, llm.StreamDoneEvent{Message: msg})
			s.Close()
		}()
		return s, nil
	}
}

func newRunCfg(stream llm.StreamFn, tools ...core.AgentTool) RunConfig {
	reg := tool.NewToolRegistry()
	for _, tl := range tools {
		_ = reg.Register(tl)
	}
	return RunConfig{
		LoopConfig: LoopConfig{Model: "fake", Stream: stream},
		Batch:      tool.BatchConfig{ToolExecutorConfig: tool.ToolExecutorConfig{Registry: reg}},
	}
}

func TestAgentLoopNoToolCallsSingleTurn(t *testing.T) {
	cfg := newRunCfg(scriptedStream([]core.AssistantMessage{
		{RoleField: core.RoleAssistant, StopReason: core.StopReasonEndTurn, Content: core.ContentList{core.NewTextContent("hi")}},
	}))
	agentCtx := &core.AgentContext{Messages: core.MessageList{core.UserMessage{RoleField: core.RoleUser}}}

	kinds, msgs := collectStream(t, agentLoop(context.Background(), agentCtx, cfg))

	want := []string{core.EventAgentStart, core.EventTurnStart, core.EventMessageEnd, core.EventTurnEnd, core.EventAgentEnd}
	assertEventKinds(t, kinds, want)
	if len(msgs) != 1 {
		t.Fatalf("run produced %d messages, want 1: %+v", len(msgs), msgs)
	}
}

func TestAgentLoopInnerLoopFeedsToolResults(t *testing.T) {
	// Turn 1: tool call. Turn 2: no tool call → inner loop ends.
	cfg := newRunCfg(scriptedStream([]core.AssistantMessage{
		oneToolAssistant("c1", "echo"),
		{RoleField: core.RoleAssistant, StopReason: core.StopReasonEndTurn, Content: core.ContentList{core.NewTextContent("done")}},
	}), echoTool("echo", core.ToolExecutionParallel, false))
	agentCtx := &core.AgentContext{Messages: core.MessageList{core.UserMessage{RoleField: core.RoleUser}}}

	kinds, msgs := collectStream(t, agentLoop(context.Background(), agentCtx, cfg))

	// Two turns; a tool executed in the first.
	if countKind(kinds, core.EventTurnStart) != 2 {
		t.Errorf("expected 2 turns, got kinds %v", kinds)
	}
	if countKind(kinds, core.EventToolExecutionEnd) != 1 {
		t.Errorf("expected 1 tool execution, got kinds %v", kinds)
	}
	// Messages produced: assistant(tool) + toolResult + assistant(done) = 3.
	if len(msgs) != 3 {
		t.Fatalf("expected 3 new messages, got %d: %+v", len(msgs), msgs)
	}
	if _, ok := msgs[1].(core.ToolResultMessage); !ok {
		t.Errorf("expected message[1] to be a tool result, got %T", msgs[1])
	}
}

func TestAgentLoopFollowUpMessagesContinue(t *testing.T) {
	served := false
	cfg := newRunCfg(scriptedStream([]core.AssistantMessage{
		{RoleField: core.RoleAssistant, StopReason: core.StopReasonEndTurn, Content: core.ContentList{core.NewTextContent("first")}},
		{RoleField: core.RoleAssistant, StopReason: core.StopReasonEndTurn, Content: core.ContentList{core.NewTextContent("second")}},
	}))
	cfg.GetFollowUpMessages = func(ctx context.Context, agentCtx *core.AgentContext) []core.AgentMessage {
		if served {
			return nil
		}
		served = true
		return []core.AgentMessage{core.UserMessage{RoleField: core.RoleUser, Content: core.ContentList{core.NewTextContent("more")}}}
	}
	agentCtx := &core.AgentContext{Messages: core.MessageList{core.UserMessage{RoleField: core.RoleUser}}}

	kinds, _ := collectStream(t, agentLoop(context.Background(), agentCtx, cfg))
	if countKind(kinds, core.EventTurnStart) != 2 {
		t.Errorf("follow-up should drive a second turn, got kinds %v", kinds)
	}
}

func TestAgentLoopShouldStopAfterTurn(t *testing.T) {
	cfg := newRunCfg(scriptedStream([]core.AssistantMessage{
		oneToolAssistant("c1", "echo"),
		{RoleField: core.RoleAssistant, StopReason: core.StopReasonEndTurn},
	}), echoTool("echo", core.ToolExecutionParallel, false))
	cfg.ShouldStopAfterTurn = func(ctx context.Context, agentCtx *core.AgentContext) bool { return true }
	agentCtx := &core.AgentContext{Messages: core.MessageList{core.UserMessage{RoleField: core.RoleUser}}}

	kinds, _ := collectStream(t, agentLoop(context.Background(), agentCtx, cfg))
	// Stops after the first turn_end, so only one turn.
	if countKind(kinds, core.EventTurnStart) != 1 {
		t.Errorf("shouldStopAfterTurn=true must stop after one turn, got %v", kinds)
	}
	if kinds[len(kinds)-1] != core.EventAgentEnd {
		t.Errorf("run must end with agent_end, got %v", kinds)
	}
}

func TestAgentLoopSteeringInjected(t *testing.T) {
	var injectedSeen bool
	steer := core.UserMessage{RoleField: core.RoleUser, Content: core.ContentList{core.NewTextContent("steer")}}
	cfg := newRunCfg(scriptedStream([]core.AssistantMessage{
		oneToolAssistant("c1", "echo"),
		{RoleField: core.RoleAssistant, StopReason: core.StopReasonEndTurn},
	}), echoTool("echo", core.ToolExecutionParallel, false))
	pulled := false
	cfg.GetSteeringMessages = func(ctx context.Context) []core.AgentMessage {
		if pulled {
			return nil
		}
		pulled = true
		return []core.AgentMessage{steer}
	}
	agentCtx := &core.AgentContext{Messages: core.MessageList{core.UserMessage{RoleField: core.RoleUser}}}
	collectStream(t, agentLoop(context.Background(), agentCtx, cfg))
	for _, m := range agentCtx.Messages {
		if um, ok := m.(core.UserMessage); ok && len(um.Content) == 1 {
			if tc, ok := um.Content[0].(core.TextContent); ok && tc.Text == "steer" {
				injectedSeen = true
			}
		}
	}
	if !injectedSeen {
		t.Errorf("steering message was not injected into the context")
	}
}

func TestAgentLoopPrepareNextTurnSwapsModel(t *testing.T) {
	var seenModels []string
	streamFn := func(ctx context.Context, model string, llmContext llm.LlmContext, cfg llm.StreamConfig) (*llm.AssistantMessageEventStream, error) {
		seenModels = append(seenModels, model)
		var msg core.AssistantMessage
		if len(seenModels) == 1 {
			msg = oneToolAssistant("c1", "echo")
		} else {
			msg = core.AssistantMessage{RoleField: core.RoleAssistant, StopReason: core.StopReasonEndTurn}
		}
		s := llm.NewAssistantMessageEventStream(0)
		go func() { _ = s.Emit(ctx, llm.StreamDoneEvent{Message: msg}); s.Close() }()
		return s, nil
	}
	cfg := newRunCfg(streamFn, echoTool("echo", core.ToolExecutionParallel, false))
	newModel := "swapped-model"
	cfg.PrepareNextTurn = func(ctx context.Context, agentCtx *core.AgentContext) *TurnUpdate {
		return &TurnUpdate{Model: &newModel}
	}
	agentCtx := &core.AgentContext{Messages: core.MessageList{core.UserMessage{RoleField: core.RoleUser}}}
	collectStream(t, agentLoop(context.Background(), agentCtx, cfg))
	if len(seenModels) != 2 || seenModels[1] != newModel {
		t.Errorf("prepareNextTurn should swap model to %q, saw %v", newModel, seenModels)
	}
}

func TestAgentLoopLengthFailsToolCalls(t *testing.T) {
	// Turn 1: tool call but truncated (length). Turn 2: end.
	truncated := oneToolAssistant("c1", "echo")
	truncated.StopReason = core.StopReasonLength
	cfg := newRunCfg(scriptedStream([]core.AssistantMessage{
		truncated,
		{RoleField: core.RoleAssistant, StopReason: core.StopReasonEndTurn},
	}), echoTool("echo", core.ToolExecutionParallel, false))
	agentCtx := &core.AgentContext{Messages: core.MessageList{core.UserMessage{RoleField: core.RoleUser}}}

	kinds, msgs := collectStream(t, agentLoop(context.Background(), agentCtx, cfg))
	// The tool must NOT have executed (truncated → failed instead).
	if countKind(kinds, core.EventToolExecutionEnd) != 0 {
		t.Errorf("truncated message must not execute tools, got %v", kinds)
	}
	// A failed tool result must have been synthesized.
	var foundFail bool
	for _, m := range msgs {
		if tr, ok := m.(core.ToolResultMessage); ok && tr.IsError && tr.ToolCallID == "c1" {
			foundFail = true
		}
	}
	if !foundFail {
		t.Errorf("expected a synthesized failed tool result for the truncated call")
	}
}

func TestAgentLoopErrorStopEndsRun(t *testing.T) {
	cfg := newRunCfg(scriptedStream([]core.AssistantMessage{
		{RoleField: core.RoleAssistant, StopReason: core.StopReasonError, ErrorMessage: "boom"},
	}))
	agentCtx := &core.AgentContext{Messages: core.MessageList{core.UserMessage{RoleField: core.RoleUser}}}

	kinds, _ := collectStream(t, agentLoop(context.Background(), agentCtx, cfg))
	if countKind(kinds, core.EventTurnStart) != 1 {
		t.Errorf("error stop must end after one turn, got %v", kinds)
	}
	if kinds[len(kinds)-1] != core.EventAgentEnd {
		t.Errorf("run must end with agent_end, got %v", kinds)
	}
}

func TestAgentLoopAllTerminateStopsRun(t *testing.T) {
	term := true
	termTool := execTool{
		name: "quit",
		run: func(ctx context.Context, id string, args json.RawMessage, onUpdate core.ToolUpdateFunc) (core.AgentToolResult, error) {
			return core.AgentToolResult{Content: core.ContentList{core.NewTextContent("bye")}, Terminate: &term}, nil
		},
	}
	cfg := newRunCfg(scriptedStream([]core.AssistantMessage{
		oneToolAssistant("c1", "quit"),
		{RoleField: core.RoleAssistant, StopReason: core.StopReasonEndTurn}, // should never be reached
	}), termTool)
	agentCtx := &core.AgentContext{Messages: core.MessageList{core.UserMessage{RoleField: core.RoleUser}}}

	kinds, _ := collectStream(t, agentLoop(context.Background(), agentCtx, cfg))
	if countKind(kinds, core.EventTurnStart) != 1 {
		t.Errorf("terminate must end the run after one turn, got %v", kinds)
	}
}

func assertEventKinds(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("event kinds = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("event[%d] = %q, want %q (full %v)", i, got[i], want[i], got)
		}
	}
}

func countKind(kinds []string, want string) int {
	n := 0
	for _, k := range kinds {
		if k == want {
			n++
		}
	}
	return n
}
