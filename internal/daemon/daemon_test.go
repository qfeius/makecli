/**
 * [INPUT]: 依赖 daemon.go/run.go/client.go/execenv.go 与 adapter 契约；net/http/httptest 模拟 gateway
 * [OUTPUT]: 对外提供执行编排回归——start→读触发→执行→事件上报→complete 的顺序与载荷、取消收尾、失败收尾
 * [POS]: internal/daemon 的测试面——对 gateway 打桩测编排，不依赖真实 CLI 与网络
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/qfeius/makecli/internal/daemon/adapter"
)

// fakeGateway 按 X-Make-Target 记录调用并回预设响应。
type fakeGateway struct {
	mu     sync.Mutex
	calls  []string
	bodies map[string][]byte
	server *httptest.Server
	events ListEventsResponse
}

func newFakeGateway(t *testing.T) *fakeGateway {
	t.Helper()
	fake := &fakeGateway{bodies: map[string][]byte{}}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := r.Header.Get(TargetHeader)
		body := make([]byte, 0)
		if r.Body != nil {
			buffer := make([]byte, 64*1024)
			n, _ := r.Body.Read(buffer)
			body = buffer[:n]
		}
		fake.mu.Lock()
		fake.calls = append(fake.calls, target)
		fake.bodies[target] = body
		fake.mu.Unlock()

		var data any = map[string]any{}
		if target == TargetListEvents {
			data = fake.events
		}
		if target == TargetAppendEvents {
			data = AppendEventsResponse{Appended: 1, NextSeq: 10}
		}
		dataJSON, _ := json.Marshal(data)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(Envelope{Code: 200, Msg: "ok", Data: dataJSON})
	}))
	return fake
}

func (f *fakeGateway) targets() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// stubBackend 返回脚本化的执行流。
type stubBackend struct {
	messages []adapter.Message
	result   adapter.Result
	gotOpts  adapter.ExecOptions
	gotText  string
}

func (b *stubBackend) Provider() string                       { return "claude-code" }
func (b *stubBackend) Detect(context.Context) (string, error) { return "stub-1.0", nil }
func (b *stubBackend) Execute(_ context.Context, prompt string, opts adapter.ExecOptions) (*adapter.Session, error) {
	b.gotOpts = opts
	b.gotText = prompt
	messages := make(chan adapter.Message, len(b.messages))
	results := make(chan adapter.Result, 1)
	for _, message := range b.messages {
		messages <- message
	}
	close(messages)
	results <- b.result
	close(results)
	return &adapter.Session{Messages: messages, Result: results}, nil
}

func userMessageEvent(seq int64, text string) Event {
	payload, _ := json.Marshal(UserMessagePayload{Blocks: []Block{{Kind: "text", Text: text}}, EndUser: "user_1"})
	return Event{Seq: seq, Type: "user_message", Payload: payload}
}

func testClaim() RunClaim {
	return RunClaim{
		RunID: "run_1", SessionID: "session_1", LeaseToken: "lease_1",
		Agent:   AgentBundle{Name: "测试", Instructions: "你是测试 agent"},
		Trigger: SeqRange{FromSeq: 0, ToSeq: 1},
	}
}

func newTestDaemon(t *testing.T, gatewayURL string) *Daemon {
	t.Helper()
	return &Daemon{
		client:         NewClient(gatewayURL, "token"),
		backends:       map[string]adapter.Backend{},
		workBaseDir:    t.TempDir(),
		maxRunDuration: time.Minute,
		logger:         slog.New(slog.NewTextHandler(testWriter{t}, nil)),
	}
}

type testWriter struct{ t *testing.T }

func (w testWriter) Write(p []byte) (int, error) { w.t.Log(string(p)); return len(p), nil }

func TestExecuteRunHappyPath(t *testing.T) {
	gateway := newFakeGateway(t)
	defer gateway.server.Close()
	gateway.events = ListEventsResponse{Events: []Event{
		userMessageEvent(0, "第一条"), userMessageEvent(1, "第二条"),
	}}
	backend := &stubBackend{
		messages: []adapter.Message{
			{Type: adapter.MessageThinking, Text: "想"},
			{Type: adapter.MessageToolUse, Tool: "Bash", CallID: "c1"},
			{Type: adapter.MessageToolResult, CallID: "c1", Output: "ok"},
		},
		result: adapter.Result{Text: "最终答复", CLISessionID: "cli_new"},
	}
	daemonUnderTest := newTestDaemon(t, gateway.server.URL)

	cancelled := false
	daemonUnderTest.executeRun(context.Background(), backend, testClaim(), &cancelled)

	if backend.gotText != "第一条\n\n第二条" {
		t.Fatalf("prompt = %q, want 触发区间合并", backend.gotText)
	}
	if backend.gotOpts.WorkDir == "" {
		t.Fatal("应准备工作目录")
	}
	targets := gateway.targets()
	// start → list → append(执行事件+最终 message) → complete
	if targets[0] != TargetStartRun || targets[1] != TargetListEvents || targets[len(targets)-1] != TargetCompleteRun {
		t.Fatalf("targets = %v", targets)
	}
	var complete CompleteRunRequest
	_ = json.Unmarshal(gateway.bodies[TargetCompleteRun], &complete)
	if complete.CLISessionID != "cli_new" || complete.WorkDir != backend.gotOpts.WorkDir {
		t.Fatalf("complete = %+v, want 连续性回写", complete)
	}
	var appended AppendEventsRequest
	_ = json.Unmarshal(gateway.bodies[TargetAppendEvents], &appended)
	hasMessage := false
	for _, event := range appended.Events {
		if event.Type == "message" {
			hasMessage = true
		}
	}
	if !hasMessage {
		t.Fatalf("最终答复应作为 message 事件上报: %+v", appended.Events)
	}
}

func TestExecuteRunFailureReportsCLICrash(t *testing.T) {
	gateway := newFakeGateway(t)
	defer gateway.server.Close()
	gateway.events = ListEventsResponse{Events: []Event{userMessageEvent(0, "hi")}}
	backend := &stubBackend{result: adapter.Result{IsError: true, ErrorMessage: "boom"}}
	daemonUnderTest := newTestDaemon(t, gateway.server.URL)

	cancelled := false
	daemonUnderTest.executeRun(context.Background(), backend, testClaim(), &cancelled)

	var fail FailRunRequest
	_ = json.Unmarshal(gateway.bodies[TargetFailRun], &fail)
	if fail.Reason != FailReasonCLICrash {
		t.Fatalf("reason = %q, want cli_crash", fail.Reason)
	}
}

func TestExecuteRunCancelledFinalizesAsCancelled(t *testing.T) {
	gateway := newFakeGateway(t)
	defer gateway.server.Close()
	gateway.events = ListEventsResponse{Events: []Event{userMessageEvent(0, "hi")}}
	backend := &stubBackend{result: adapter.Result{IsError: true, ErrorMessage: "被杀"}}
	daemonUnderTest := newTestDaemon(t, gateway.server.URL)

	cancelled := true // 心跳 actions 已置位
	daemonUnderTest.executeRun(context.Background(), backend, testClaim(), &cancelled)

	var fail FailRunRequest
	_ = json.Unmarshal(gateway.bodies[TargetFailRun], &fail)
	if fail.Reason != FailReasonCancelled {
		t.Fatalf("reason = %q, want cancelled（取消收尾优先于错误原因）", fail.Reason)
	}
}
