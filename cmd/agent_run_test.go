/**
 * [INPUT]: 依赖 agent_run.go、newAgentCmd，encoding/json、errors、net/http、net/http/httptest、strings、testing
 * [OUTPUT]: 覆盖 run 参数迁移、token 优先级、编程/纯聊天一次性和 REPL 的请求及输出
 * [POS]: cmd/agent run 的命令集成测试，httptest 模拟流式网关，临时目录隔离工具与信任存储
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package cmd

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentRunFlags(t *testing.T) {
	cmd := newAgentRunCmd()
	for name, want := range map[string]string{
		"approve": "false", "chat-only": "false", "model": "default", "token": "",
		"gateway-server-url": "", "prompt": "", "system": "",
	} {
		flag := cmd.Flags().Lookup(name)
		if flag == nil || flag.DefValue != want {
			t.Errorf("flag --%s = %v, want default %q", name, flag, want)
		}
	}
	if cmd.Flags().ShorthandLookup("p") != cmd.Flags().Lookup("prompt") {
		t.Error("-p must map to --prompt")
	}
	if cmd.Flags().Lookup("no-tools") != nil {
		t.Error("obsolete --no-tools must not be registered")
	}
}

func TestAgentRunRequiresToken(t *testing.T) {
	t.Setenv("MAKE_AGENT_TOKEN", "")
	cmd := newAgentCmd()
	cmd.SetArgs([]string{"run", "-p", "hi"})
	if err := cmd.Execute(); !errors.Is(err, errAgentTokenMissing) {
		t.Errorf("error = %v, want missing token guidance", err)
	}
}

func TestAgentRunRouting(t *testing.T) {
	for _, tc := range []struct {
		name, input, flagToken, wantToken string
		chatOnly, once                    bool
	}{
		{"code once flag overrides env", "", "flag-token", "flag-token", false, true},
		{"chat once env token", "", "", "env-token", true, true},
		{"code REPL", "hi\n/exit\n", "flag-token", "flag-token", false, false},
		{"chat REPL", "hi\n/exit\n", "flag-token", "flag-token", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			t.Setenv("HOME", t.TempDir())
			t.Setenv("MAKE_CLI_CONFIG_DIR", t.TempDir())
			t.Setenv("MAKE_AGENT_TOKEN", "env-token")
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/v1/chat/completions" || r.Header.Get("Authorization") != "Bearer "+tc.wantToken {
					t.Errorf("unexpected gateway routing: %s %s", r.Method, r.URL)
				}
				var body struct {
					Model    string
					Stream   bool
					Tools    []any
					Messages []struct{ Role, Content string }
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.Model != "test-model" || !body.Stream || len(body.Messages) < 2 || body.Messages[len(body.Messages)-1].Content != "hi" || !strings.Contains(body.Messages[0].Content, "test instruction") {
					t.Errorf("unexpected request: %+v", body)
				}
				wantTools := 7
				if tc.chatOnly {
					wantTools = 0
				}
				if len(body.Tools) != wantTools {
					t.Errorf("tools = %d, want %d", len(body.Tools), wantTools)
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"hello from run\"}}]}\n\ndata: {\"choices\":[{\"delta\":{},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n"))
			}))
			defer srv.Close()
			cmd := newAgentCmd()
			args := []string{"run", "--gateway-server-url", srv.URL, "--model", "test-model", "--system", "test instruction", "--approve"}
			if tc.flagToken != "" {
				args = append(args, "--token", tc.flagToken)
			}
			if tc.chatOnly {
				args = append(args, "--chat-only")
			}
			if tc.once {
				args = append(args, "-p", "hi")
			}
			cmd.SetArgs(args)
			cmd.SetIn(strings.NewReader(tc.input))
			var out strings.Builder
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			if err := cmd.Execute(); err != nil {
				t.Fatal(err)
			}
			if calls != 1 || !strings.Contains(out.String(), "hello from run") {
				t.Errorf("calls = %d, output = %s", calls, out.String())
			}
			if !tc.once && !strings.Contains(out.String(), "makecli agent run (model:") {
				t.Errorf("REPL banner must name run: %s", out.String())
			}
		})
	}
}

func TestAgentRunInvalidArgs(t *testing.T) {
	for _, args := range [][]string{{"run", "unexpected"}, {"--approve"}, {"--chat-only"}, {"unknown"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			cmd := newAgentCmd()
			cmd.SetArgs(args)
			if err := cmd.Execute(); err == nil {
				t.Error("expected invalid args error")
			}
		})
	}
}
