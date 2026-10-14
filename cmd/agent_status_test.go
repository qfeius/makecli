/**
 * [INPUT]: 依赖 agent_status.go、agentCmd、agentCreateTestRoot、captureStdout、internal/api，encoding/json、net/http、net/http/httptest、strings、testing
 * [OUTPUT]: 覆盖 enable/disable 的状态更新、参数顺序、应用范围、凭证、输出、无效输入和失败回执
 * [POS]: cmd/agent 启停命令集成测试，httptest 隔离远端副作用
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qfeius/makecli/internal/api"
)

func TestAgentStatusRemote(t *testing.T) {
	for _, action := range []string{"enable", "disable"} {
		for _, output := range []string{outputTable, outputJSON, outputAuto} {
			t.Run(action+"/"+output, func(t *testing.T) {
				status, calls := action+"d", 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if r.Method != http.MethodPost || r.URL.Path != "/api/make/agent/v1/agent" || r.Header.Get("X-Make-Target") != "MakeService.UpdateResource" {
						t.Errorf("unexpected routing: %s %s", r.Method, r.URL)
					}
					if r.Header.Get("Authorization") != "Bearer override-token" {
						t.Error("must use global access token")
					}
					var body map[string]string
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if len(body) != 3 || body["key"] != "poc_agent" || body["appKey"] != "agent_poc_beta_" || body["status"] != status {
						t.Errorf("must update only status: %+v", body)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{
						"key": "poc_agent", "appKey": "agent_poc_beta_", "status": status,
						"name": "POC Agent", "prompt": "unchanged", "modelKey": "deepseek",
						"plugins": []map[string]any{{"id": "binding_01", "pluginKey": "mcp_make", "enabled": true, "config": map[string]any{}}},
					}})
				}))
				defer srv.Close()
				root := agentCreateTestRoot(t, srv.URL)
				parent, _, _ := root.Find([]string{"agent"})
				parent.AddCommand(newAgentStatusCmd(action, status))
				args := []string{"agent", "--app", "agent_poc_beta_", action, "poc_agent"}
				if output == outputTable {
					args = []string{"agent", action, "poc_agent", "--app", "agent_poc_beta_"}
				}
				root.SetArgs(append(args, "--output", output, "--access-token", "override-token"))
				out := captureStdout(t, func() {
					if err := root.Execute(); err != nil {
						t.Fatal(err)
					}
				})
				if calls != 1 {
					t.Fatalf("calls = %d, want 1", calls)
				}
				if output == outputTable {
					if want := "Agent 'poc_agent' " + status + " successfully in app 'agent_poc_beta_'\n"; out != want {
						t.Errorf("output = %q, want %q", out, want)
					}
					return
				}
				var saved api.Agent
				if err := json.Unmarshal([]byte(out), &saved); err != nil {
					t.Fatal(err)
				}
				if saved.Status != status || saved.Prompt != "unchanged" || saved.ModelKey != "deepseek" || len(saved.Plugins) != 1 {
					t.Errorf("incomplete saved Agent: %+v", saved)
				}
			})
		}
		child, _, err := agentCmd.Find([]string{action})
		if err != nil || child == agentCmd || child.Name() != action {
			t.Fatalf("missing registered command %s: %v", action, err)
		}
	}
}

func TestAgentStatusWithoutCredentials(t *testing.T) {
	agentCreateTestRoot(t, "http://unused")
	t.Setenv(EnvAccessToken, "")
	if err := runAgentStatus("poc_agent", "agent_poc", "enabled", outputJSON); err == nil {
		t.Fatal("expected missing credentials error")
	}
}

func TestAgentStatusInvalidInput(t *testing.T) {
	for _, action := range []string{"enable", "disable"} {
		for _, args := range [][]string{
			{}, {"poc_agent"}, {"poc_agent", "--app", ""}, {"poc_agent", "--app", " "},
			{"poc_agent", "--app", strings.Repeat("a", 129)}, {"_bad", "--app", "demo"},
			{"poc_agent", "extra", "--app", "demo"}, {"poc_agent", "--app", "demo", "--output", "yaml"},
		} {
			t.Run(action+"/"+strings.Join(args, " "), func(t *testing.T) {
				calls := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
				defer srv.Close()
				root := agentCreateTestRoot(t, srv.URL)
				parent, _, _ := root.Find([]string{"agent"})
				parent.AddCommand(newAgentStatusCmd(action, action+"d"))
				root.SetArgs(append([]string{"agent", action}, args...))
				if err := root.Execute(); err == nil {
					t.Error("expected input error")
				}
				if calls != 0 {
					t.Errorf("invalid input made %d requests", calls)
				}
			})
		}
	}
}

func TestAgentStatusFailure(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"code":404,"msg":"agent not found","data":{"reason":"NOT_FOUND"}}`, "agent not found"},
		{`{"code":403,"msg":"forbidden","data":{"reason":"FORBIDDEN"}}`, "forbidden"},
		{`{"code":401,"msg":"unauthorized","data":{}}`, "鉴权失败"},
		{`{"code":200,"data":{}}`, "不一致"},
	} {
		t.Run(tc.want, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) }))
			defer srv.Close()
			agentCreateTestRoot(t, srv.URL)
			out := captureStdout(t, func() {
				if err := runAgentStatus("poc_agent", "agent_poc", "enabled", outputJSON); err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Errorf("error = %v, want %q", err, tc.want)
				}
			})
			if out != "" {
				t.Errorf("failure printed success: %s", out)
			}
		})
	}
}
