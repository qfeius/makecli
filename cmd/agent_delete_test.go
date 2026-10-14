/**
 * [INPUT]: 依赖 agent_delete.go、agentCmd、agentCreateTestRoot、captureStdout，encoding/json、errors、net/http、net/http/httptest、strings、testing
 * [OUTPUT]: 覆盖 Agent 删除命令注册、路由、应用范围、凭证、输出、输入校验、确认门禁与失败回执
 * [POS]: cmd/agent delete 的命令集成测试，httptest 隔离真实后端删除
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

func TestAgentDeleteRemote(t *testing.T) {
	for _, output := range []string{outputTable, outputJSON, outputAuto} {
		t.Run(output, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/api/make/agent/v1/agent" || r.Header.Get("X-Make-Target") != "MakeService.DeleteResource" {
					t.Errorf("unexpected delete routing: %s %s", r.Method, r.URL)
				}
				if r.Header.Get("Authorization") != "Bearer override-token" {
					t.Error("delete must use the global access token")
				}
				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if len(body) != 2 || body["key"] != "invoice_agent" || body["appKey"] != "invoice_app_beta_" {
					t.Errorf("unexpected delete body: %+v", body)
				}
				_, _ = w.Write([]byte(`{"code":200,"msg":"success","data":{}}`))
			}))
			defer srv.Close()
			root := agentCreateTestRoot(t, srv.URL)
			parent, _, _ := root.Find([]string{"agent"})
			parent.AddCommand(newAgentDeleteCmd())
			root.SetArgs([]string{"agent", "delete", "invoice_agent", "--app", "invoice_app_beta_", "--output", output, "--access-token", "override-token", "--yes"})
			out := captureStdout(t, func() {
				if err := root.Execute(); err != nil {
					t.Fatal(err)
				}
			})
			if calls != 1 {
				t.Fatalf("calls = %d, want 1", calls)
			}
			if output == outputTable {
				if out != "Agent 'invoice_agent' deleted successfully from app 'invoice_app_beta_'\n" {
					t.Errorf("unexpected text: %s", out)
				}
				return
			}
			var result struct {
				Key, AppKey string
				Deleted     bool
			}
			if err := json.Unmarshal([]byte(out), &result); err != nil {
				t.Fatal(err)
			}
			if result.Key != "invoice_agent" || result.AppKey != "invoice_app_beta_" || !result.Deleted {
				t.Errorf("unexpected JSON: %s", out)
			}
		})
	}
	child, _, err := agentCmd.Find([]string{"delete"})
	if err != nil || child == agentCmd || child.Name() != "delete" {
		t.Fatalf("real agent parent must register delete: %v", err)
	}
}

func TestAgentDeleteInvalidInput(t *testing.T) {
	for _, args := range [][]string{
		{}, {"invoice_agent"}, {"invoice_agent", "--app", ""},
		{"invoice_agent", "--app", " "}, {"invoice_agent", "--app", strings.Repeat("a", 129)},
		{"_invalid", "--app", "demo"}, {strings.Repeat("a", 21), "--app", "demo"},
		{"invoice_agent", "extra", "--app", "demo"}, {"invoice_agent", "--app", "demo", "--output", "yaml"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
			defer srv.Close()
			root := agentCreateTestRoot(t, srv.URL)
			parent, _, _ := root.Find([]string{"agent"})
			parent.AddCommand(newAgentDeleteCmd())
			root.SetArgs(append([]string{"agent", "delete"}, args...))
			if err := root.Execute(); err == nil {
				t.Error("expected invalid input error")
			}
			if calls != 0 {
				t.Errorf("invalid input made %d calls", calls)
			}
		})
	}
}

func TestAgentDeleteFailure(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"not found", `{"code":404,"msg":"agent not found","data":{"reason":"NOT_FOUND"}}`, "agent not found"},
		{"forbidden", `{"code":403,"msg":"forbidden","data":{"reason":"FORBIDDEN"}}`, "forbidden"},
		{"auth", `{"code":401,"msg":"unauthorized","data":{}}`, "鉴权失败"},
		{"malformed", `{`, "响应"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			agentCreateTestRoot(t, srv.URL)
			out := captureStdout(t, func() {
				if err := runAgentDelete("invoice_agent", "invoice_app", outputJSON, true); err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Errorf("error = %v, want %q", err, tc.want)
				}
			})
			if out != "" {
				t.Errorf("failure printed a success result: %s", out)
			}
		})
	}
}

func TestAgentDeleteWithoutCredentials(t *testing.T) {
	agentCreateTestRoot(t, "http://unused")
	t.Setenv(EnvAccessToken, "")
	if err := runAgentDelete("invoice_agent", "invoice_app", outputJSON, true); err == nil {
		t.Fatal("expected missing credentials error")
	}
}

func TestAgentDeleteConfirmation(t *testing.T) {
	for _, tc := range []struct {
		name string
		yes  string
		err  error
	}{
		{"confirmed", "", nil},
		{"cancelled", "", errors.New("deletion cancelled")},
		{"long flag skips confirmation", "--yes", errors.New("confirmation must be skipped")},
		{"short flag skips confirmation", "-y", errors.New("confirmation must be skipped")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, confirms := 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				_, _ = w.Write([]byte(`{"code":200,"data":{}}`))
			}))
			defer srv.Close()
			original := confirmAgentDeleteFunc
			confirmAgentDeleteFunc = func(key, appKey string) error {
				confirms++
				if key != "invoice_agent" || appKey != "invoice_app" {
					t.Errorf("unexpected confirmation target: %s, %s", key, appKey)
				}
				return tc.err
			}
			t.Cleanup(func() { confirmAgentDeleteFunc = original })
			root := agentCreateTestRoot(t, srv.URL)
			parent, _, _ := root.Find([]string{"agent"})
			parent.AddCommand(newAgentDeleteCmd())
			args := []string{"agent", "delete", "--app", "invoice_app", "invoice_agent", "--output", "table"}
			if tc.yes != "" {
				args = append(args, tc.yes)
			}
			root.SetArgs(args)
			out := captureStdout(t, func() {
				err := root.Execute()
				if tc.yes == "" && tc.err != nil {
					if !errors.Is(err, tc.err) {
						t.Errorf("error = %v, want %v", err, tc.err)
					}
				} else if err != nil {
					t.Error(err)
				}
			})
			wantConfirms, wantCalls := 1, 1
			if tc.yes != "" {
				wantConfirms = 0
			} else if tc.err != nil {
				wantCalls = 0
				if out != "" {
					t.Errorf("cancelled deletion printed success: %s", out)
				}
			}
			if confirms != wantConfirms || calls != wantCalls {
				t.Errorf("confirmations/requests = %d/%d, want %d/%d", confirms, calls, wantConfirms, wantCalls)
			}
		})
	}
}

func TestAgentDeleteNonInteractive(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer srv.Close()
	agentCreateTestRoot(t, srv.URL)
	err := runAgentDelete("invoice_agent", "invoice_app", outputJSON, false)
	if err == nil || !strings.Contains(err.Error(), "--yes") || !strings.Contains(err.Error(), `agent "invoice_agent" from app "invoice_app"`) {
		t.Fatalf("expected non-interactive confirmation refusal, got %v", err)
	}
	if calls != 0 {
		t.Errorf("unconfirmed deletion made %d requests", calls)
	}
}
