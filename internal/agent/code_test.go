/**
 * [INPUT]: 依赖 testing、httptest、encoding/json、strings、sync；被测对象 code.go/render.go
 * [OUTPUT]: code agent 编排的单元测试——无头文本轮、工具调用轮（read 执行且结果回喂）、
 *           未信任目录副作用工具拦截（无头）、--approve 放行、REPL 确认 y/n、/clear 清史、
 *           网关错误转终结失败（退出码 1 语义）
 * [POS]: internal/agent 的会话编排测试，脚本化 SSE 假 gateway 隔离网络，
 *        t.TempDir + MAKE_CLI_CONFIG_DIR 隔离文件系统与信任存储
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// scriptedGateway replays one scripted SSE response per request, in order, and
// records every decoded request body for assertions.
type scriptedGateway struct {
	mu    sync.Mutex
	turns [][]string
	reqs  []map[string]any
	srv   *httptest.Server
}

func newScriptedGateway(t *testing.T, turns ...[]string) *scriptedGateway {
	t.Helper()
	g := &scriptedGateway{turns: turns}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		idx := len(g.reqs)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		g.reqs = append(g.reqs, body)
		var lines []string
		if idx < len(g.turns) {
			lines = g.turns[idx]
		} else {
			lines = sseText("")
		}
		g.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		for _, l := range lines {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", l)
		}
	}))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *scriptedGateway) request(t *testing.T, i int) map[string]any {
	t.Helper()
	g.mu.Lock()
	defer g.mu.Unlock()
	if i >= len(g.reqs) {
		t.Fatalf("gateway saw %d requests, want index %d", len(g.reqs), i)
	}
	return g.reqs[i]
}

func (g *scriptedGateway) requestCount() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.reqs)
}

// sseText scripts a pure-text turn ending with finish_reason=stop.
func sseText(text string) []string {
	quoted, _ := json.Marshal(text)
	return []string{
		fmt.Sprintf(`{"choices":[{"delta":{"content":%s}}]}`, quoted),
		`{"choices":[{"delta":{},"finish_reason":"stop"}]}`,
		`[DONE]`,
	}
}

// sseToolCall scripts a single-tool-call turn ending with finish_reason=tool_calls.
func sseToolCall(id, name, args string) []string {
	quotedArgs, _ := json.Marshal(args)
	return []string{
		fmt.Sprintf(`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":%q,"function":{"name":%q,"arguments":%s}}]}}]}`,
			id, name, quotedArgs),
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`[DONE]`,
	}
}

// newCodeOpts builds CodeOptions rooted in a fresh temp dir with the trust
// store isolated under MAKE_CLI_CONFIG_DIR.
func newCodeOpts(t *testing.T, g *scriptedGateway) CodeOptions {
	t.Helper()
	t.Setenv("MAKE_CLI_CONFIG_DIR", t.TempDir())
	return CodeOptions{
		GatewayURL: g.srv.URL,
		Token:      "tok",
		Model:      "default",
		Dir:        t.TempDir(),
	}
}

// messagesOf returns the decoded messages array of a captured request body.
func messagesOf(t *testing.T, req map[string]any) []map[string]any {
	t.Helper()
	raw, _ := req["messages"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, m := range raw {
		mm, _ := m.(map[string]any)
		out = append(out, mm)
	}
	return out
}

// findRole returns the first message with the given role, or nil.
func findRole(msgs []map[string]any, role string) map[string]any {
	for _, m := range msgs {
		if m["role"] == role {
			return m
		}
	}
	return nil
}

// TestRunCodeOnceText: 无头模式纯文本轮——流式文本落到输出、请求带 system 提示
// 词（含基座与 AGENTS.md）与工具声明、自然收尾返回 nil。
func TestRunCodeOnceText(t *testing.T) {
	g := newScriptedGateway(t, sseText("hello from agent"))
	opts := newCodeOpts(t, g)
	if err := os.WriteFile(filepath.Join(opts.Dir, "AGENTS.md"), []byte("Always answer in haiku."), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer

	if err := RunCodeOnce(context.Background(), opts, "hi", &out); err != nil {
		t.Fatalf("RunCodeOnce: %v", err)
	}
	if !strings.Contains(out.String(), "hello from agent") {
		t.Errorf("output missing streamed text: %q", out.String())
	}

	msgs := messagesOf(t, g.request(t, 0))
	sys := findRole(msgs, "system")
	if sys == nil {
		t.Fatal("request missing system message")
	}
	sysText, _ := sys["content"].(string)
	if !strings.Contains(sysText, "You are makecli agent") || !strings.Contains(sysText, "Always answer in haiku.") {
		t.Errorf("system prompt missing base/AGENTS.md: %q", sysText)
	}
	if user := findRole(msgs, "user"); user == nil || user["content"] != "hi" {
		t.Errorf("user message = %v", user)
	}
	tools, _ := g.request(t, 0)["tools"].([]any)
	if len(tools) != 7 {
		t.Errorf("declared tools = %d, want 7", len(tools))
	}
}

// TestRunCodeOnceToolRead: 工具调用轮——read 真执行（root=Dir），结果作为
// role:"tool" 回喂第二轮，输出含 ⚙ 行与最终文本。
func TestRunCodeOnceToolRead(t *testing.T) {
	g := newScriptedGateway(t,
		sseToolCall("call_1", "read", `{"path":"f.txt"}`),
		sseText("file says: secret-content"),
	)
	opts := newCodeOpts(t, g)
	if err := os.WriteFile(filepath.Join(opts.Dir, "f.txt"), []byte("secret-content"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer

	if err := RunCodeOnce(context.Background(), opts, "read f.txt", &out); err != nil {
		t.Fatalf("RunCodeOnce: %v", err)
	}
	if g.requestCount() != 2 {
		t.Fatalf("gateway requests = %d, want 2 (tool round trip)", g.requestCount())
	}
	toolMsg := findRole(messagesOf(t, g.request(t, 1)), "tool")
	if toolMsg == nil {
		t.Fatal("second request missing role:tool result message")
	}
	if toolMsg["tool_call_id"] != "call_1" {
		t.Errorf("tool_call_id = %v", toolMsg["tool_call_id"])
	}
	if content, _ := toolMsg["content"].(string); !strings.Contains(content, "secret-content") {
		t.Errorf("tool result content = %q", content)
	}
	rendered := out.String()
	if !strings.Contains(rendered, "⚙ read: f.txt") {
		t.Errorf("output missing tool call line: %q", rendered)
	}
	if !strings.Contains(rendered, "file says: secret-content") {
		t.Errorf("output missing final text: %q", rendered)
	}
}

// TestRunCodeOnceBashBlockedUntrusted: 无头模式未信任目录的副作用工具直接
// 拦截——bash 不执行、拦截语（含 --approve 指引）回喂模型。
func TestRunCodeOnceBashBlockedUntrusted(t *testing.T) {
	g := newScriptedGateway(t,
		sseToolCall("call_1", "bash", `{"command":"touch marker.txt"}`),
		sseText("blocked, understood"),
	)
	opts := newCodeOpts(t, g)
	var out bytes.Buffer

	if err := RunCodeOnce(context.Background(), opts, "touch it", &out); err != nil {
		t.Fatalf("RunCodeOnce: %v", err)
	}
	if _, err := os.Stat(filepath.Join(opts.Dir, "marker.txt")); !os.IsNotExist(err) {
		t.Error("bash must not run in an untrusted directory (marker.txt exists)")
	}
	toolMsg := findRole(messagesOf(t, g.request(t, 1)), "tool")
	if toolMsg == nil {
		t.Fatal("second request missing blocked tool result")
	}
	content, _ := toolMsg["content"].(string)
	if !strings.Contains(content, "blocked") || !strings.Contains(content, "--approve") {
		t.Errorf("blocked message = %q, want blocked + --approve hint", content)
	}
	if !strings.Contains(out.String(), "✗") {
		t.Errorf("output missing error preview marker: %q", out.String())
	}
}

// TestRunCodeOnceBashApproved: --approve 授予会话信任后，无头模式副作用工具
// 直接放行执行。
func TestRunCodeOnceBashApproved(t *testing.T) {
	g := newScriptedGateway(t,
		sseToolCall("call_1", "bash", `{"command":"echo ok > marker.txt"}`),
		sseText("done"),
	)
	opts := newCodeOpts(t, g)
	opts.Approve = true
	var out bytes.Buffer

	if err := RunCodeOnce(context.Background(), opts, "touch it", &out); err != nil {
		t.Fatalf("RunCodeOnce: %v", err)
	}
	if _, err := os.Stat(filepath.Join(opts.Dir, "marker.txt")); err != nil {
		t.Errorf("bash should have run with --approve: %v", err)
	}
	if !strings.Contains(out.String(), "⚙ bash: echo ok > marker.txt") {
		t.Errorf("output missing bash tool line: %q", out.String())
	}
}

// TestRunCodeOnceGatewayError: 网关 401——终结失败作为 error 返回（cmd 出口
// 转退出码 1），错误文本还原 OpenAI 风格错误体。
func TestRunCodeOnceGatewayError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid token","type":"auth_error"}}`))
	}))
	defer srv.Close()
	t.Setenv("MAKE_CLI_CONFIG_DIR", t.TempDir())
	opts := CodeOptions{GatewayURL: srv.URL, Token: "bad", Model: "default", Dir: t.TempDir()}
	var out bytes.Buffer

	err := RunCodeOnce(context.Background(), opts, "hi", &out)
	if err == nil {
		t.Fatal("expected terminal failure error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid token") {
		t.Errorf("error = %v, want gateway error body surfaced", err)
	}
}

// TestRunCodeREPLConfirmDeny: REPL 确认 n=拒绝——bash 不执行，拦截结果回喂，
// 循环不退出（后续 /exit 正常收尾）。
func TestRunCodeREPLConfirmDeny(t *testing.T) {
	g := newScriptedGateway(t,
		sseToolCall("call_1", "bash", `{"command":"touch marker.txt"}`),
		sseText("ok, skipped"),
	)
	opts := newCodeOpts(t, g)
	var out bytes.Buffer
	input := strings.NewReader("do it\nn\n/exit\n")

	if err := RunCodeREPL(context.Background(), opts, input, &out); err != nil {
		t.Fatalf("RunCodeREPL: %v", err)
	}
	if _, err := os.Stat(filepath.Join(opts.Dir, "marker.txt")); !os.IsNotExist(err) {
		t.Error("denied bash must not run (marker.txt exists)")
	}
	rendered := out.String()
	if !strings.Contains(rendered, "允许?") {
		t.Errorf("output missing confirmation prompt: %q", rendered)
	}
	toolMsg := findRole(messagesOf(t, g.request(t, 1)), "tool")
	if toolMsg == nil {
		t.Fatal("second request missing denied tool result")
	}
	if content, _ := toolMsg["content"].(string); !strings.Contains(content, "denied") {
		t.Errorf("denied message = %q", content)
	}
}

// TestRunCodeREPLConfirmAllowOnce: REPL 确认 y=允许一次——bash 执行；同目录
// 第二次副作用调用仍需确认（未授予会话信任），再次 y 后照常执行。
func TestRunCodeREPLConfirmAllowOnce(t *testing.T) {
	g := newScriptedGateway(t,
		sseToolCall("call_1", "bash", `{"command":"echo 1 > m1.txt"}`),
		sseText("first done"),
		sseToolCall("call_2", "bash", `{"command":"echo 2 > m2.txt"}`),
		sseText("second done"),
	)
	opts := newCodeOpts(t, g)
	var out bytes.Buffer
	input := strings.NewReader("one\ny\ntwo\ny\n/exit\n")

	if err := RunCodeREPL(context.Background(), opts, input, &out); err != nil {
		t.Fatalf("RunCodeREPL: %v", err)
	}
	for _, f := range []string{"m1.txt", "m2.txt"} {
		if _, err := os.Stat(filepath.Join(opts.Dir, f)); err != nil {
			t.Errorf("bash marker %s missing after y confirmation: %v", f, err)
		}
	}
	// y 只放行一次：确认提示必然出现两次。
	if got := strings.Count(out.String(), "允许?"); got != 2 {
		t.Errorf("confirmation prompts = %d, want 2 (y grants once, not session)", got)
	}
}

// TestRunCodeREPLConfirmAlways: REPL 确认 a=本会话全允许——后续副作用调用不再
// 询问。
func TestRunCodeREPLConfirmAlways(t *testing.T) {
	g := newScriptedGateway(t,
		sseToolCall("call_1", "bash", `{"command":"echo 1 > m1.txt"}`),
		sseText("first done"),
		sseToolCall("call_2", "bash", `{"command":"echo 2 > m2.txt"}`),
		sseText("second done"),
	)
	opts := newCodeOpts(t, g)
	var out bytes.Buffer
	input := strings.NewReader("one\na\ntwo\n/exit\n")

	if err := RunCodeREPL(context.Background(), opts, input, &out); err != nil {
		t.Fatalf("RunCodeREPL: %v", err)
	}
	for _, f := range []string{"m1.txt", "m2.txt"} {
		if _, err := os.Stat(filepath.Join(opts.Dir, f)); err != nil {
			t.Errorf("bash marker %s missing: %v", f, err)
		}
	}
	if got := strings.Count(out.String(), "允许?"); got != 1 {
		t.Errorf("confirmation prompts = %d, want 1 (a grants session trust)", got)
	}
}

// TestRunCodeREPLClearHistory: /clear 清空历史——下一轮请求只带 system + 新
// user 消息，不携带此前对话。
func TestRunCodeREPLClearHistory(t *testing.T) {
	g := newScriptedGateway(t, sseText("first"), sseText("second"))
	opts := newCodeOpts(t, g)
	var out bytes.Buffer
	input := strings.NewReader("one\n/clear\ntwo\n/exit\n")

	if err := RunCodeREPL(context.Background(), opts, input, &out); err != nil {
		t.Fatalf("RunCodeREPL: %v", err)
	}
	if !strings.Contains(out.String(), "(历史已清空)") {
		t.Errorf("output missing /clear ack: %q", out.String())
	}
	msgs := messagesOf(t, g.request(t, 1))
	if len(msgs) != 2 {
		t.Fatalf("post-/clear request messages = %d, want 2 (system+user): %v", len(msgs), msgs)
	}
	if user := findRole(msgs, "user"); user == nil || user["content"] != "two" {
		t.Errorf("post-/clear user message = %v", user)
	}
}

// TestRunCodeREPLKeepsHistory: 不 /clear 时每条输入接续同一消息历史。
func TestRunCodeREPLKeepsHistory(t *testing.T) {
	g := newScriptedGateway(t, sseText("first"), sseText("second"))
	opts := newCodeOpts(t, g)
	var out bytes.Buffer
	input := strings.NewReader("one\ntwo\n/exit\n")

	if err := RunCodeREPL(context.Background(), opts, input, &out); err != nil {
		t.Fatalf("RunCodeREPL: %v", err)
	}
	msgs := messagesOf(t, g.request(t, 1))
	// system + user(one) + assistant(first) + user(two) = 4。
	if len(msgs) != 4 {
		t.Fatalf("second request messages = %d, want 4: %v", len(msgs), msgs)
	}
	if asst := findRole(msgs, "assistant"); asst == nil || asst["content"] != "first" {
		t.Errorf("history assistant message = %v", asst)
	}
}
