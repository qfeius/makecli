/**
 * [INPUT]: 依赖 agent_create.go、agent_list.go、agentCmd、stdout/client 测试 helper，encoding/json、net/http、net/http/httptest、os、path/filepath、strings、testing、github.com/spf13/cobra
 * [OUTPUT]: 覆盖 Agent 创建分发、应用归属、固定运行服务、输入文件、默认值、错误和输出
 * [POS]: cmd/agent create 的端到端命令测试，通过 httptest 验证真实 HTTP 请求，不创建远端资源
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func agentCreateTestRoot(t *testing.T, url string) *cobra.Command {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MAKE_CLI_CONFIG_DIR", t.TempDir())
	t.Setenv(EnvAccessToken, "test-token")
	setProfile(t, "default")
	setMetaServerURLFlag(t, url)
	oldToken, oldContext := AccessToken, Context
	AccessToken, Context = "", "dev"
	t.Cleanup(func() { AccessToken, Context = oldToken, oldContext })
	root := &cobra.Command{Use: "makecli", SilenceErrors: true, SilenceUsage: true}
	root.PersistentFlags().StringVar(&AccessToken, "access-token", "", "test access token")
	root.PersistentFlags().StringVar(&Context, "context", "dev", "test context")
	parent := &cobra.Command{Use: "agent", RunE: func(cmd *cobra.Command, args []string) error {
		t.Fatal("create must not start the local chat agent")
		return nil
	}}
	parent.AddCommand(newAgentCreateCmd(), newAgentListCmd())
	root.AddCommand(parent)
	return root
}

func TestAgentCreateRemote(t *testing.T) {
	for _, tc := range []struct{ output, appKey string }{
		{outputJSON, "expense_app"},
		{outputTable, "expense_app"},
		{outputJSON, "expense_app_beta"},
	} {
		t.Run(tc.output+"/"+tc.appKey, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/api/make/agent/v1/agent" || r.Header.Get("X-Make-Target") != "MakeService.CreateResource" {
					t.Errorf("unexpected endpoint/routing: %s %s", r.Method, r.URL)
				}
				if r.Header.Get("Authorization") != "Bearer override-token" {
					t.Error("must use the inherited global access token")
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				for key, want := range map[string]string{
					"key": "expense_agent", "name": "expense_agent", "modelKey": "deepseek",
					"appKey": tc.appKey, "runtimeKey": "make-runtime-service", "status": "disabled",
				} {
					if body[key] != want {
						t.Errorf("%s = %v, want %s", key, body[key], want)
					}
				}
				if _, exists := body["productKey"]; exists {
					t.Error("request must not contain productKey")
				}
				body["prompt"], body["plugins"], body["meta"] = "", []any{}, map[string]any{}
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "success", "data": body})
			}))
			defer srv.Close()
			root := agentCreateTestRoot(t, srv.URL)
			root.SetArgs([]string{"agent", "create", "expense_agent", "--app", tc.appKey, "--model", "deepseek", "--access-token", "override-token", "--context", "dev", "--output", tc.output})
			out := captureStdout(t, func() {
				if err := root.Execute(); err != nil {
					t.Error(err)
				}
			})
			if calls != 1 {
				t.Fatalf("calls = %d, want 1", calls)
			}
			if tc.output == outputJSON {
				var saved map[string]any
				if err := json.Unmarshal([]byte(out), &saved); err != nil || saved["key"] != "expense_agent" || saved["status"] != "disabled" || saved["appKey"] != tc.appKey || saved["productKey"] != nil {
					t.Errorf("invalid JSON result: %s (%v)", out, err)
				}
			} else if !strings.Contains(out, "created successfully") || !strings.Contains(out, "disabled") || !strings.Contains(out, "App") || !strings.Contains(out, tc.appKey) {
				t.Errorf("unexpected text output: %s", out)
			}
		})
	}
	child, _, err := agentCmd.Find([]string{"create"})
	if err != nil || child == agentCmd || child.Name() != "create" {
		t.Fatalf("real agent parent must register create: %v", err)
	}
	for _, name := range []string{"product-key", "runtime-key", "token", "model-key", "chat-only"} {
		if child.Flags().Lookup(name) != nil {
			t.Errorf("create must not expose --%s", name)
		}
	}
}

func TestAgentCreateAppKeyLength(t *testing.T) {
	for _, appKey := range []string{"a", strings.Repeat("a", 128), strings.Repeat("应", 128)} {
		req, err := agentCreateRequest("demo", agentCreateOptions{appKey: appKey, modelKey: "deepseek", status: "disabled"})
		if err != nil || req.AppKey != appKey {
			t.Errorf("appKey must preserve 1–128 characters: request = %+v, err = %v", req, err)
		}
	}
}

func TestAgentCreateInputFiles(t *testing.T) {
	dir := t.TempDir()
	promptPath, pluginsPath := filepath.Join(dir, "prompt.txt"), filepath.Join(dir, "plugins.json")
	if err := os.WriteFile(promptPath, []byte("按实际结果回答。\n"), 0600); err != nil {
		t.Fatal(err)
	}
	pluginsJSON := `[{"pluginKey":"mcp_make","enabled":false,"config":{}}]`
	if err := os.WriteFile(pluginsPath, []byte(pluginsJSON), 0600); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{pluginsJSON, "@" + pluginsPath, "-"} {
		t.Run(source, func(t *testing.T) {
			oldStdin := flagStdin
			flagStdin = strings.NewReader(pluginsJSON)
			defer func() { flagStdin = oldStdin }()
			req, err := agentCreateRequest("expense_agent", agentCreateOptions{
				appKey: "expense_app", modelKey: "deepseek", name: "报销助手", description: "报销查询", status: "enabled",
				promptFile: promptPath, pluginsJSON: source,
			})
			if err != nil {
				t.Fatal(err)
			}
			if req.Prompt != "按实际结果回答。\n" || req.Name != "报销助手" || req.Description != "报销查询" || req.Status != "enabled" {
				t.Errorf("unexpected request: %+v", req)
			}
			if len(req.Plugins) != 1 || req.Plugins[0].Enabled == nil || *req.Plugins[0].Enabled || req.Plugins[0].Config == nil {
				t.Errorf("unexpected plugins: %+v", req.Plugins)
			}
		})
	}
}

func TestAgentCreateInvalidInput(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"missing key", []string{"--app", "expense_app", "--model", "deepseek"}},
		{"extra key", []string{"--app", "expense_app", "demo", "extra", "--model", "deepseek"}},
		{"invalid key", []string{"--app", "expense_app", "_bad", "--model", "deepseek"}},
		{"missing model", []string{"--app", "expense_app", "demo"}},
		{"empty model", []string{"--app", "expense_app", "demo", "--model", ""}},
		{"blank name", []string{"--app", "expense_app", "demo", "--model", "deepseek", "--name", " "}},
		{"invalid status", []string{"--app", "expense_app", "demo", "--model", "deepseek", "--status", "active"}},
		{"invalid output", []string{"--app", "expense_app", "demo", "--model", "deepseek", "--output", "yaml"}},
		{"prompt conflict", []string{"--app", "expense_app", "demo", "--model", "deepseek", "--prompt", "", "--prompt-file", "prompt.txt"}},
		{"missing prompt file", []string{"--app", "expense_app", "demo", "--model", "deepseek", "--prompt-file", "/nonexistent/prompt.txt"}},
		{"bad plugins", []string{"--app", "expense_app", "demo", "--model", "deepseek", "--plugins-json", "{"}},
		{"null plugins", []string{"--app", "expense_app", "demo", "--model", "deepseek", "--plugins-json", "null"}},
		{"update plugin id", []string{"--app", "expense_app", "demo", "--model", "deepseek", "--plugins-json", `[{"id":"binding_01","pluginKey":"mcp_make","config":{}}]`}},
		{"removed product flag", []string{"--app", "expense_app", "demo", "--model", "deepseek", "--product-key", "other"}},
		{"fixed runtime", []string{"--app", "expense_app", "demo", "--model", "deepseek", "--runtime-key", "other"}},
		{"missing app", []string{"demo", "--model", "deepseek"}},
		{"empty app", []string{"demo", "--app", "", "--model", "deepseek"}},
		{"blank app", []string{"demo", "--app", " ", "--model", "deepseek"}},
		{"long app", []string{"demo", "--app", strings.Repeat("a", 129), "--model", "deepseek"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
			defer srv.Close()
			root := agentCreateTestRoot(t, srv.URL)
			root.SetArgs(append([]string{"agent", "create"}, tc.args...))
			if err := root.Execute(); err == nil {
				t.Error("expected invalid input error")
			}
			if calls != 0 {
				t.Errorf("invalid input made %d remote calls", calls)
			}
		})
	}
}

func TestAgentCreateAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"code":409,"msg":"Agent already exists","data":{"reason":"KEY_CONFLICT"}}`))
	}))
	defer srv.Close()
	root := agentCreateTestRoot(t, srv.URL)
	root.SetArgs([]string{"agent", "create", "demo", "--app", "expense_app", "--model", "deepseek"})
	out := captureStdout(t, func() {
		if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "Agent already exists") {
			t.Errorf("unexpected error: %v", err)
		}
	})
	if out != "" {
		t.Errorf("failure must not print success: %s", out)
	}
}
