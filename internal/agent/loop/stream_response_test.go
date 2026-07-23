// 移植自 github.com/smallnest/pigo (MIT License, Copyright (c) 2026 smallnest)，按 makecli 需要裁剪改写。
package loop

import (
	"context"
	"testing"

	"github.com/qfeius/makecli/internal/agent/core"
	"github.com/qfeius/makecli/internal/agent/llm"
)

// fakeStream builds a StreamFn that replays a fixed sequence of events, pushing
// each onto an AssistantMessageEventStream from a producer goroutine.
func fakeStream(events []llm.AssistantMessageEvent) llm.StreamFn {
	return func(ctx context.Context, model string, llmContext llm.LlmContext, cfg llm.StreamConfig) (*llm.AssistantMessageEventStream, error) {
		s := llm.NewAssistantMessageEventStream(0)
		go func() {
			for _, ev := range events {
				if err := s.Emit(ctx, ev); err != nil {
					s.SetError(err)
					s.Close()
					return
				}
			}
			s.Close()
		}()
		return s, nil
	}
}

// drives streamAssistantResponse with a synchronous emit that records events.
func runStream(t *testing.T, agentCtx *core.AgentContext, cfg LoopConfig) (core.AssistantMessage, []core.AgentEvent) {
	t.Helper()
	var got []core.AgentEvent
	emit := func(ctx context.Context, ev core.AgentEvent) error {
		got = append(got, ev)
		return nil
	}
	msg, err := streamAssistantResponse(context.Background(), agentCtx, cfg, emit)
	if err != nil {
		t.Fatalf("streamAssistantResponse: %v", err)
	}
	return msg, got
}

func TestStreamResponseBackfillAndEvents(t *testing.T) {
	partial0 := core.AssistantMessage{RoleField: core.RoleAssistant}
	partial1 := core.AssistantMessage{RoleField: core.RoleAssistant, Content: core.ContentList{core.NewTextContent("hel")}}
	final := core.AssistantMessage{RoleField: core.RoleAssistant, Content: core.ContentList{core.NewTextContent("hello")}, StopReason: core.StopReasonEndTurn}

	cfg := LoopConfig{
		Model: "fake",
		Stream: fakeStream([]llm.AssistantMessageEvent{
			llm.StreamStartEvent{Partial: partial0},
			llm.StreamTextEvent{Partial: partial1},
			llm.StreamDoneEvent{Message: final},
		}),
	}
	agentCtx := &core.AgentContext{Messages: core.MessageList{core.UserMessage{RoleField: core.RoleUser}}}

	msg, events := runStream(t, agentCtx, cfg)

	if msg.StopReason != core.StopReasonEndTurn {
		t.Errorf("final stopReason = %q, want end_turn", msg.StopReason)
	}
	// Context should hold the user message + the final assistant message (the
	// placeholder was replaced, not appended twice).
	if len(agentCtx.Messages) != 2 {
		t.Fatalf("context messages = %d, want 2: %+v", len(agentCtx.Messages), agentCtx.Messages)
	}
	last, ok := agentCtx.Messages[1].(core.AssistantMessage)
	if !ok || len(last.Content) != 1 {
		t.Fatalf("last message not final assistant: %+v", agentCtx.Messages[1])
	}
	// Event order: message_start, message_update, message_end.
	wantKinds := []string{core.EventMessageStart, core.EventMessageUpdate, core.EventMessageEnd}
	if len(events) != len(wantKinds) {
		t.Fatalf("event count = %d, want %d: %+v", len(events), len(wantKinds), events)
	}
	for i, w := range wantKinds {
		if events[i].EventType() != w {
			t.Errorf("event[%d] = %q, want %q", i, events[i].EventType(), w)
		}
	}
}

func TestStreamResponseErrorEvent(t *testing.T) {
	errMsg := core.AssistantMessage{RoleField: core.RoleAssistant, StopReason: core.StopReasonError, ErrorMessage: "boom"}
	cfg := LoopConfig{
		Model:  "fake",
		Stream: fakeStream([]llm.AssistantMessageEvent{llm.StreamErrorEvent{Message: errMsg}}),
	}
	agentCtx := &core.AgentContext{}
	msg, events := runStream(t, agentCtx, cfg)
	if msg.StopReason != core.StopReasonError || msg.ErrorMessage != "boom" {
		t.Errorf("want error terminal message, got %+v", msg)
	}
	// No start event was sent; error should still append the terminal message.
	if len(agentCtx.Messages) != 1 {
		t.Fatalf("context messages = %d, want 1", len(agentCtx.Messages))
	}
	if events[len(events)-1].EventType() != core.EventMessageEnd {
		t.Errorf("last event = %q, want message_end", events[len(events)-1].EventType())
	}
}

func TestStreamResponseDynamicAPIKey(t *testing.T) {
	var seenKey string
	streamFn := func(ctx context.Context, model string, llmContext llm.LlmContext, cfg llm.StreamConfig) (*llm.AssistantMessageEventStream, error) {
		seenKey = cfg.APIKey
		s := llm.NewAssistantMessageEventStream(0)
		go func() {
			_ = s.Emit(ctx, llm.StreamDoneEvent{Message: core.AssistantMessage{RoleField: core.RoleAssistant, StopReason: core.StopReasonEndTurn}})
			s.Close()
		}()
		return s, nil
	}
	cfg := LoopConfig{
		Model:     "fake",
		APIKey:    "static-key",
		Provider:  "test",
		Stream:    streamFn,
		GetAPIKey: func(ctx context.Context, provider string) string { return "dynamic-key" },
	}
	runStream(t, &core.AgentContext{}, cfg)
	if seenKey != "dynamic-key" {
		t.Errorf("dynamic key not used: got %q", seenKey)
	}

	// Empty dynamic key falls back to static.
	cfg.GetAPIKey = func(ctx context.Context, provider string) string { return "" }
	runStream(t, &core.AgentContext{}, cfg)
	if seenKey != "static-key" {
		t.Errorf("fallback to static key failed: got %q", seenKey)
	}
}

func TestStreamResponseTransformAndConvertOrder(t *testing.T) {
	var order []string
	cfg := LoopConfig{
		Model: "fake",
		TransformContext: func(ctx context.Context, msgs core.MessageList) core.MessageList {
			order = append(order, "transform")
			return msgs
		},
		ConvertToLlm: func(msgs core.MessageList) core.MessageList {
			order = append(order, "convert")
			return msgs
		},
		Stream: func(ctx context.Context, model string, llmContext llm.LlmContext, cfg llm.StreamConfig) (*llm.AssistantMessageEventStream, error) {
			order = append(order, "stream")
			s := llm.NewAssistantMessageEventStream(0)
			go func() {
				_ = s.Emit(ctx, llm.StreamDoneEvent{Message: core.AssistantMessage{RoleField: core.RoleAssistant}})
				s.Close()
			}()
			return s, nil
		},
	}
	runStream(t, &core.AgentContext{}, cfg)
	if len(order) != 3 || order[0] != "transform" || order[1] != "convert" || order[2] != "stream" {
		t.Errorf("call order wrong: %v", order)
	}
}

func TestStreamResponseEarlyBuildFailure(t *testing.T) {
	cfg := LoopConfig{
		Model: "fake",
		Stream: func(ctx context.Context, model string, llmContext llm.LlmContext, cfg llm.StreamConfig) (*llm.AssistantMessageEventStream, error) {
			return nil, context.DeadlineExceeded
		},
	}
	msg, _ := runStream(t, &core.AgentContext{}, cfg)
	if msg.StopReason != core.StopReasonError {
		t.Errorf("early build failure should yield error message, got %+v", msg)
	}
}
