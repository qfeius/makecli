/**
 * [INPUT]: 依赖 testing、httptest、encoding/json、strings；被测对象 gateway.go
 * [OUTPUT]: GatewayProvider 的单元测试——请求编码（system/messages/tool_calls/tool 结果/tools/stream 选项/头部）、
 *           SSE 解码（纯文本流、tool_calls 跨 chunk 分片、finish_reason 映射、usage）、
 *           错误语义（非 200 返回 error、流中坏 chunk 化为 StreamErrorEvent、[DONE] 缺失 EOF 收尾）
 * [POS]: internal/agent/llm 的行为契约测试，httptest 隔离网络；语义对齐 loop/faux_provider_test.go
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qfeius/makecli/internal/agent/core"
)

// sseLines writes SSE data lines to w, flushing after each.
func sseLines(w http.ResponseWriter, lines ...string) {
	w.Header().Set("Content-Type", "text/event-stream")
	fl, _ := w.(http.Flusher)
	for _, l := range lines {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", l)
		if fl != nil {
			fl.Flush()
		}
	}
}

// drainStream collects every event and the final result of a provider stream.
func drainStream(t *testing.T, s *AssistantMessageEventStream) ([]AssistantMessageEvent, core.AssistantMessage, error) {
	t.Helper()
	var events []AssistantMessageEvent
	for ev := range s.Events() {
		events = append(events, ev)
	}
	res, err := s.Result(context.Background())
	return events, res, err
}

// fakeTool is a minimal AgentTool for asserting tool declarations on the wire.
type fakeTool struct{ name string }

func (f fakeTool) Name() string        { return f.name }
func (f fakeTool) Description() string { return "desc of " + f.name }
func (f fakeTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}}}`)
}
func (f fakeTool) ExecutionMode() core.ToolExecutionMode { return core.ToolExecutionParallel }
func (f fakeTool) Execute(ctx context.Context, id string, args json.RawMessage, onUpdate core.ToolUpdateFunc) (core.AgentToolResult, error) {
	return core.AgentToolResult{}, nil
}

// TestGatewayTextStream drives a pure text SSE stream end to end: request
// encoding (headers, system prompt, stream options), start/text deltas, and a
// done event with mapped stop reason + usage.
func TestGatewayTextStream(t *testing.T) {
	var gotBody map[string]any
	var gotHeader http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Clone()
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		sseLines(w,
			`{"id":"resp-1","model":"real-model","choices":[{"delta":{"content":"Hel"}}]}`,
			`{"choices":[{"delta":{"content":"lo"}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
			`{"usage":{"prompt_tokens":10,"completion_tokens":5}}`,
			`[DONE]`,
		)
	}))
	defer server.Close()

	p := NewGatewayProvider(server.URL, "tok-1", "sess-1")
	stream, err := p.StreamCompletion(context.Background(), CompletionRequest{
		Model: "default",
		Context: LlmContext{
			SystemPrompt: "be brief",
			Messages: core.MessageList{
				core.UserMessage{RoleField: core.RoleUser, Content: core.ContentList{core.NewTextContent("hi")}},
			},
		},
	})
	if err != nil {
		t.Fatalf("StreamCompletion: %v", err)
	}
	events, final, resErr := drainStream(t, stream)
	if resErr != nil {
		t.Fatalf("stream result error: %v", resErr)
	}

	// Headers与 client.go 契约一致。
	if got := gotHeader.Get("Authorization"); got != "Bearer tok-1" {
		t.Errorf("Authorization = %q", got)
	}
	if got := gotHeader.Get("X-Session-ID"); got != "sess-1" {
		t.Errorf("X-Session-ID = %q", got)
	}
	if got := gotHeader.Get("Accept"); got != "text/event-stream" {
		t.Errorf("Accept = %q", got)
	}

	// 请求体：model 别名、stream+include_usage、system+user 消息、无工具则无 tools。
	if gotBody["model"] != "default" || gotBody["stream"] != true {
		t.Errorf("body model/stream = %v/%v", gotBody["model"], gotBody["stream"])
	}
	so, _ := gotBody["stream_options"].(map[string]any)
	if so == nil || so["include_usage"] != true {
		t.Errorf("stream_options = %v", gotBody["stream_options"])
	}
	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages len = %d, want 2 (system+user)", len(msgs))
	}
	if m0, _ := msgs[0].(map[string]any); m0["role"] != "system" || m0["content"] != "be brief" {
		t.Errorf("messages[0] = %v", msgs[0])
	}
	if m1, _ := msgs[1].(map[string]any); m1["role"] != "user" || m1["content"] != "hi" {
		t.Errorf("messages[1] = %v", msgs[1])
	}
	if _, hasTools := gotBody["tools"]; hasTools {
		t.Error("tools must be omitted when no tools are declared")
	}

	// 事件形状：start → 两个 text delta（累积）→ done。
	if len(events) < 4 {
		t.Fatalf("expected >=4 events, got %d: %v", len(events), events)
	}
	if _, ok := events[0].(StreamStartEvent); !ok {
		t.Errorf("events[0] = %T, want StreamStartEvent", events[0])
	}
	txt1, ok1 := events[1].(StreamTextEvent)
	txt2, ok2 := events[2].(StreamTextEvent)
	if !ok1 || !ok2 {
		t.Fatalf("events[1..2] = %T,%T, want StreamTextEvent", events[1], events[2])
	}
	if got := core.ContentToText(txt1.Partial.Content); got != "Hel" {
		t.Errorf("first text partial = %q", got)
	}
	if got := core.ContentToText(txt2.Partial.Content); got != "Hello" {
		t.Errorf("second text partial = %q", got)
	}
	if _, ok := events[len(events)-1].(StreamDoneEvent); !ok {
		t.Errorf("last event = %T, want StreamDoneEvent", events[len(events)-1])
	}

	// 终值：文本、stop 映射 end_turn、usage、响应诊断字段。
	if got := core.ContentToText(final.Content); got != "Hello" {
		t.Errorf("final text = %q", got)
	}
	if final.StopReason != core.StopReasonEndTurn {
		t.Errorf("final stop reason = %q, want end_turn", final.StopReason)
	}
	if final.Usage == nil || final.Usage.InputTokens != 10 || final.Usage.OutputTokens != 5 {
		t.Errorf("final usage = %+v", final.Usage)
	}
	if final.ResponseID != "resp-1" || final.ResponseModel != "real-model" {
		t.Errorf("response id/model = %q/%q", final.ResponseID, final.ResponseModel)
	}
	if final.Provider != "gateway" || final.Model != "default" {
		t.Errorf("provider/model = %q/%q", final.Provider, final.Model)
	}
}

// TestGatewayToolCallAssembly covers tool_calls arriving in fragments across
// chunks (id/name in the first fragment, arguments split over several) plus the
// finish_reason=tool_calls mapping.
func TestGatewayToolCallAssembly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sseLines(w,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"read","arguments":"{\"pa"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"th\":\"a"}}]}}]}`,
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":".txt\"}"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`[DONE]`,
		)
	}))
	defer server.Close()

	p := NewGatewayProvider(server.URL, "tok", "sess")
	stream, err := p.StreamCompletion(context.Background(), CompletionRequest{Model: "default"})
	if err != nil {
		t.Fatalf("StreamCompletion: %v", err)
	}
	events, final, resErr := drainStream(t, stream)
	if resErr != nil {
		t.Fatalf("stream result error: %v", resErr)
	}

	toolEvents := 0
	for _, ev := range events {
		if _, ok := ev.(StreamToolCallEvent); ok {
			toolEvents++
		}
	}
	if toolEvents != 3 {
		t.Errorf("tool-call delta events = %d, want 3", toolEvents)
	}

	calls := final.ToolCalls()
	if len(calls) != 1 {
		t.Fatalf("final tool calls = %d, want 1: %+v", len(calls), final.Content)
	}
	if calls[0].ID != "call_1" || calls[0].Name != "read" {
		t.Errorf("tool call id/name = %q/%q", calls[0].ID, calls[0].Name)
	}
	if got := string(calls[0].Arguments); got != `{"path":"a.txt"}` {
		t.Errorf("assembled arguments = %s", got)
	}
	if final.StopReason != core.StopReasonToolUse {
		t.Errorf("stop reason = %q, want tool_use", final.StopReason)
	}
}

// TestGatewayEncodesHistoryAndTools locks the wire shape of a multi-turn
// context: assistant with tool_calls, tool result as role:"tool", and the
// function-tool declarations.
func TestGatewayEncodesHistoryAndTools(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		sseLines(w, `{"choices":[{"delta":{"content":"ok"},"finish_reason":"stop"}]}`, `[DONE]`)
	}))
	defer server.Close()

	p := NewGatewayProvider(server.URL, "tok", "sess")
	stream, err := p.StreamCompletion(context.Background(), CompletionRequest{
		Model: "default",
		Context: LlmContext{
			Messages: core.MessageList{
				core.UserMessage{RoleField: core.RoleUser, Content: core.ContentList{core.NewTextContent("read it")}},
				core.AssistantMessage{RoleField: core.RoleAssistant, Content: core.ContentList{
					core.NewTextContent("reading"),
					core.NewToolCallContent("call_1", "read", json.RawMessage(`{"path":"a.txt"}`)),
				}},
				core.ToolResultMessage{RoleField: core.RoleToolResult, ToolCallID: "call_1", ToolName: "read",
					Content: core.ContentList{core.NewTextContent("file body")}},
			},
			Tools: []core.AgentTool{fakeTool{name: "read"}},
		},
	})
	if err != nil {
		t.Fatalf("StreamCompletion: %v", err)
	}
	if _, _, resErr := drainStream(t, stream); resErr != nil {
		t.Fatalf("stream result error: %v", resErr)
	}

	msgs, _ := gotBody["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages len = %d, want 3", len(msgs))
	}
	asst, _ := msgs[1].(map[string]any)
	if asst["role"] != "assistant" || asst["content"] != "reading" {
		t.Errorf("assistant entry = %v", asst)
	}
	tcs, _ := asst["tool_calls"].([]any)
	if len(tcs) != 1 {
		t.Fatalf("assistant tool_calls = %v", asst["tool_calls"])
	}
	tc0, _ := tcs[0].(map[string]any)
	fn, _ := tc0["function"].(map[string]any)
	if tc0["id"] != "call_1" || tc0["type"] != "function" || fn["name"] != "read" || fn["arguments"] != `{"path":"a.txt"}` {
		t.Errorf("tool_calls[0] = %v", tc0)
	}
	toolMsg, _ := msgs[2].(map[string]any)
	if toolMsg["role"] != "tool" || toolMsg["tool_call_id"] != "call_1" || toolMsg["content"] != "file body" {
		t.Errorf("tool result entry = %v", toolMsg)
	}
	tools, _ := gotBody["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %v", gotBody["tools"])
	}
	tool0, _ := tools[0].(map[string]any)
	tfn, _ := tool0["function"].(map[string]any)
	if tool0["type"] != "function" || tfn["name"] != "read" || tfn["description"] != "desc of read" {
		t.Errorf("tools[0] = %v", tool0)
	}
	if params, ok := tfn["parameters"].(map[string]any); !ok || params["type"] != "object" {
		t.Errorf("tools[0].parameters = %v", tfn["parameters"])
	}
}

// TestGatewayHTTPErrorReturnsError: a non-200 response means the stream was
// never established → a returned Go error carrying the OpenAI-style error body
// (FR-13 的「连接建立前失败」分支)。
func TestGatewayHTTPErrorReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid token","type":"auth_error"}}`))
	}))
	defer server.Close()

	p := NewGatewayProvider(server.URL, "bad", "sess")
	stream, err := p.StreamCompletion(context.Background(), CompletionRequest{Model: "default"})
	if err == nil {
		t.Fatal("expected error for 401 response, got nil")
	}
	if stream != nil {
		t.Error("stream must be nil on pre-stream failure")
	}
	for _, want := range []string{"401", "auth_error", "invalid token"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q must contain %q", err, want)
		}
	}
}

// TestGatewayEOFWithoutDone: [DONE] 缺失时 EOF 一样以 done 收尾，已积累的部分
// 响应不丢（对齐 pigo OpenAIDecoder.Finish）。
func TestGatewayEOFWithoutDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sseLines(w, `{"choices":[{"delta":{"content":"partial answer"}}]}`)
		// 不发 [DONE]，直接断流。
	}))
	defer server.Close()

	p := NewGatewayProvider(server.URL, "tok", "sess")
	stream, err := p.StreamCompletion(context.Background(), CompletionRequest{Model: "default"})
	if err != nil {
		t.Fatalf("StreamCompletion: %v", err)
	}
	events, final, resErr := drainStream(t, stream)
	if resErr != nil {
		t.Fatalf("stream result error: %v", resErr)
	}
	if _, ok := events[len(events)-1].(StreamDoneEvent); !ok {
		t.Fatalf("last event = %T, want StreamDoneEvent", events[len(events)-1])
	}
	if got := core.ContentToText(final.Content); got != "partial answer" {
		t.Errorf("final text = %q", got)
	}
	if final.StopReason != core.StopReasonEndTurn {
		t.Errorf("stop reason = %q, want end_turn", final.StopReason)
	}
}

// TestGatewayMidStreamErrorEvent: 流开始后的失败（坏 chunk）以 StreamErrorEvent
// + stopReason=error 收尾，不是 Go error（FR-13 的运行期分支）。
func TestGatewayMidStreamErrorEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sseLines(w,
			`{"choices":[{"delta":{"content":"so far"}}]}`,
			`{not valid json`,
		)
	}))
	defer server.Close()

	p := NewGatewayProvider(server.URL, "tok", "sess")
	stream, err := p.StreamCompletion(context.Background(), CompletionRequest{Model: "default"})
	if err != nil {
		t.Fatalf("StreamCompletion must not return a Go error once streaming: %v", err)
	}
	events, final, resErr := drainStream(t, stream)
	if resErr != nil {
		t.Fatalf("stream result error: %v", resErr)
	}
	last, ok := events[len(events)-1].(StreamErrorEvent)
	if !ok {
		t.Fatalf("last event = %T, want StreamErrorEvent", events[len(events)-1])
	}
	if last.Err == nil {
		t.Error("StreamErrorEvent.Err must be set")
	}
	if final.StopReason != core.StopReasonError {
		t.Errorf("stop reason = %q, want error", final.StopReason)
	}
	if final.ErrorMessage == "" {
		t.Error("final ErrorMessage must be set")
	}
	// 已积累的部分文本保留在终结消息上。
	if got := core.ContentToText(final.Content); got != "so far" {
		t.Errorf("final text = %q, want accumulated partial", got)
	}
}
