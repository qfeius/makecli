/**
 * [INPUT]: 依赖 testing、bytes、context、os、path/filepath、strings；被测对象 bang.go 与两条 REPL 路径
 * [OUTPUT]: 本地命令直通的单元测试——前缀解析、code REPL 与纯聊天 REPL 里 `!cmd`
 *           执行本机 shell 且零 LLM 请求、转录进历史、非零退出状态行、裸 `!` 提示、
 *           不过目录信任门控、超长输出截断
 * [POS]: internal/agent 的测试面（本地直通），复用 code_test.go 的脚本化假 gateway
 *        与 repl_test.go 的回显假 gateway，t.TempDir + MAKE_CLI_CONFIG_DIR 隔离
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestBangCommandParse: 前缀解析——`!cmd` / `! cmd` 都取到命令，裸 `!` 取到空串，
// 非 `!` 开头一律不是直通行。
func TestBangCommandParse(t *testing.T) {
	cases := []struct {
		line    string
		want    string
		wantHit bool
	}{
		{"!ls", "ls", true},
		{"! ls -la", "ls -la", true},
		{"!  git status  ", "git status", true},
		{"!", "", true},
		{"!  ", "", true},
		{"ls", "", false},
		{"", "", false},
		{"/exit", "", false},
		{"帮我看看 !ls", "", false},
	}
	for _, tc := range cases {
		got, hit := bangCommand(tc.line)
		if hit != tc.wantHit || got != tc.want {
			t.Errorf("bangCommand(%q) = (%q, %v), want (%q, %v)", tc.line, got, hit, tc.want, tc.wantHit)
		}
	}
}

// TestCodeREPLBangRunsLocallyWithoutLLM: code REPL 里 `!cmd` 在本机执行、输出直写，
// 且这一行不发起任何 LLM 请求；转录随后作为 user 消息进历史被下一轮携带。
func TestCodeREPLBangRunsLocallyWithoutLLM(t *testing.T) {
	g := newScriptedGateway(t, sseText("看到了"))
	opts := newCodeOpts(t, g)
	var out bytes.Buffer
	input := strings.NewReader("!echo hello-bang\n刚才输出了什么\n/exit\n")

	if err := RunCodeREPL(context.Background(), opts, input, &out); err != nil {
		t.Fatalf("RunCodeREPL: %v", err)
	}

	rendered := out.String()
	if !strings.Contains(rendered, "$ echo hello-bang") {
		t.Errorf("output missing echoed command line: %q", rendered)
	}
	if !strings.Contains(rendered, "hello-bang") {
		t.Errorf("output missing command stdout: %q", rendered)
	}
	// 两行输入里只有第二行该触网：直通行零 LLM 请求。
	if got := g.requestCount(); got != 1 {
		t.Fatalf("gateway requests = %d, want 1 (bang line must not hit the LLM)", got)
	}
	// system + user(转录) + user(提问) = 3。
	msgs := messagesOf(t, g.request(t, 0))
	if len(msgs) != 3 {
		t.Fatalf("request messages = %d, want 3 (system + bang transcript + prompt): %v", len(msgs), msgs)
	}
	transcript, _ := msgs[1]["content"].(string)
	if !strings.Contains(transcript, "$ echo hello-bang") || !strings.Contains(transcript, "hello-bang") {
		t.Errorf("bang transcript missing from history: %q", transcript)
	}
	if !strings.Contains(transcript, "ran a shell command locally") {
		t.Errorf("transcript missing framing sentence: %q", transcript)
	}
}

// TestCodeREPLBangSkipsTrustGate: 直通命令是用户亲手敲的，不走目录信任确认——
// 未信任目录里照样执行，且不弹 y/n/a 提示。
func TestCodeREPLBangSkipsTrustGate(t *testing.T) {
	g := newScriptedGateway(t)
	opts := newCodeOpts(t, g)
	var out bytes.Buffer
	input := strings.NewReader("!touch marker.txt\n/exit\n")

	if err := RunCodeREPL(context.Background(), opts, input, &out); err != nil {
		t.Fatalf("RunCodeREPL: %v", err)
	}
	if _, err := os.Stat(filepath.Join(opts.Dir, "marker.txt")); err != nil {
		t.Errorf("bang command must run in the session dir: %v", err)
	}
	if strings.Contains(out.String(), "允许?") {
		t.Errorf("user-typed bang must not ask for trust confirmation: %q", out.String())
	}
	if got := g.requestCount(); got != 0 {
		t.Errorf("gateway requests = %d, want 0", got)
	}
}

// TestCodeREPLBangNonZeroExit: 非零退出补一行 ✗ 状态（输出已流式打过不重复），
// 状态同样进转录。
func TestCodeREPLBangNonZeroExit(t *testing.T) {
	g := newScriptedGateway(t, sseText("知道了"))
	opts := newCodeOpts(t, g)
	var out bytes.Buffer
	input := strings.NewReader("!echo boom; exit 3\n为什么失败\n/exit\n")

	if err := RunCodeREPL(context.Background(), opts, input, &out); err != nil {
		t.Fatalf("RunCodeREPL: %v", err)
	}
	rendered := out.String()
	if !strings.Contains(rendered, "✗ bash: command exited with code 3") {
		t.Errorf("output missing exit-code status line: %q", rendered)
	}
	// BashTool 的错误消息把完整输出附在首行之后；状态行只取首行，不重复已流式打过的输出。
	for _, line := range strings.Split(rendered, "\n") {
		if strings.Contains(line, "✗") && strings.Contains(line, "boom") {
			t.Errorf("status line must not repeat the streamed output: %q", line)
		}
	}
	transcript, _ := messagesOf(t, g.request(t, 0))[1]["content"].(string)
	if !strings.Contains(transcript, "boom") || !strings.Contains(transcript, "exited with code 3") {
		t.Errorf("failed-command transcript = %q", transcript)
	}
}

// TestCodeREPLBareBangHint: 裸 `!` 只打用法提示——不执行、不触网、不进历史。
func TestCodeREPLBareBangHint(t *testing.T) {
	g := newScriptedGateway(t, sseText("hi"))
	opts := newCodeOpts(t, g)
	var out bytes.Buffer
	input := strings.NewReader("!\n你好\n/exit\n")

	if err := RunCodeREPL(context.Background(), opts, input, &out); err != nil {
		t.Fatalf("RunCodeREPL: %v", err)
	}
	if !strings.Contains(out.String(), bangHint) {
		t.Errorf("output missing usage hint: %q", out.String())
	}
	msgs := messagesOf(t, g.request(t, 0))
	if len(msgs) != 2 {
		t.Fatalf("request messages = %d, want 2 (system + prompt; bare ! adds nothing): %v", len(msgs), msgs)
	}
}

// TestChatOnlyREPLBang: --chat-only 的纯聊天 REPL 同样支持直通——本机执行、零 LLM
// 请求，转录进历史被下一轮携带。
func TestChatOnlyREPLBang(t *testing.T) {
	server, rounds := newEchoGateway(t)
	var out bytes.Buffer
	input := strings.NewReader("!echo chat-bang\n刚才呢\n/exit\n")

	err := RunREPL(context.Background(), NewClient(server.URL, "tok", "s"), "default", "", input, &out)
	if err != nil {
		t.Fatalf("RunREPL: %v", err)
	}
	if !strings.Contains(out.String(), "chat-bang") {
		t.Errorf("output missing command stdout: %q", out.String())
	}
	if len(*rounds) != 1 {
		t.Fatalf("rounds = %d, want 1 (bang line must not hit the LLM)", len(*rounds))
	}
	first := (*rounds)[0]
	if len(first) != 2 {
		t.Fatalf("messages = %d, want 2 (bang transcript + prompt): %+v", len(first), first)
	}
	if !strings.Contains(first[0].Content, "chat-bang") {
		t.Errorf("bang transcript missing from history: %q", first[0].Content)
	}
}

// TestBangHistoryEntryTruncates: 超长输出截头保留并标注；空输出有明确占位。
func TestBangHistoryEntryTruncates(t *testing.T) {
	entry := bangHistoryEntry("cat big", strings.Repeat("x", bangHistoryMaxChars+100))
	if !strings.Contains(entry, "…(output truncated)") {
		t.Error("oversized transcript must be marked truncated")
	}
	if n := len([]rune(entry)); n > bangHistoryMaxChars+200 {
		t.Errorf("truncated entry length = %d runes, want ≈%d", n, bangHistoryMaxChars)
	}
	if empty := bangHistoryEntry("true", ""); !strings.Contains(empty, "(no output)") {
		t.Errorf("empty transcript entry = %q", empty)
	}
}
