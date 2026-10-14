/**
 * [INPUT]: 依赖 agent_list.go、agentCmd、agent_create_test.go 的隔离命令 helper、captureStdout，encoding/json、net/http、net/http/httptest、strings、testing
 * [OUTPUT]: 覆盖 Agent 列表应用范围、分页、概要输出、空列表及失败无输出
 * [POS]: cmd/agent list 的端到端命令测试，httptest 隔离真实后端
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentListRemote(t *testing.T) {
	for _, tc := range []struct{ name, output, appKey string }{
		{"json", outputJSON, "expense_app"},
		{"table", outputTable, "expense_app"},
		{"auto in pipe", outputAuto, "expense_app_beta"},
		{"escaped app", outputJSON, `app'\\key`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/api/make/agent/v1/agent" || r.Header.Get("X-Make-Target") != "MakeService.ListResources" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				if r.Header.Get("Authorization") != "Bearer override-token" {
					t.Error("list must use the global access token")
				}
				var body struct {
					Filter     struct{ Expression string } `json:"filter"`
					Pagination struct{ Page, Size int }    `json:"pagination"`
					AppKey     *string                     `json:"appKey"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				want := "appKey == 'expense_app'"
				if tc.appKey == "expense_app_beta" {
					want = "appKey == 'expense_app_beta'"
				}
				if tc.name == "escaped app" {
					want = `appKey == 'app\'\\\\key'`
				}
				if body.AppKey != nil || body.Filter.Expression != want || body.Pagination.Page != 2 || body.Pagination.Size != 10 {
					t.Errorf("unexpected body: %+v, want filter %q", body, want)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code": 200, "msg": "success",
					"data":       []map[string]any{{"key": "expense_agent", "name": "报销助手", "description": "报销查询", "appKey": tc.appKey, "modelKey": "deepseek", "runtimeKey": "make-runtime-service", "status": "disabled", "meta": map[string]string{"createdAt": "2026-10-13T00:00:00Z", "updatedAt": "2026-10-13T00:00:00Z"}}},
					"pagination": map[string]int{"page": 2, "size": 10, "total": 21},
				})
			}))
			defer srv.Close()
			root := agentCreateTestRoot(t, srv.URL)
			root.SetArgs([]string{"agent", "list", "--app", tc.appKey, "--page", "2", "--size", "10", "--output", tc.output, "--access-token", "override-token"})
			out := captureStdout(t, func() {
				if err := root.Execute(); err != nil {
					t.Error(err)
				}
			})
			if calls != 1 {
				t.Fatalf("calls = %d, want 1", calls)
			}
			if tc.output == outputTable {
				for _, text := range []string{"KEY", "NAME", "DESCRIPTION", "MODEL", "STATUS", "CREATED AT", "expense_agent", "报销助手", "报销查询", "deepseek", "disabled", "2026-10-13", "Showing 1 of 21 agents"} {
					if !strings.Contains(out, text) {
						t.Errorf("missing %q in table: %s", text, out)
					}
				}
				return
			}
			var result struct {
				Data       []map[string]any                       `json:"data"`
				Pagination struct{ Count, Page, Size, Total int } `json:"pagination"`
			}
			if err := json.Unmarshal([]byte(out), &result); err != nil {
				t.Fatalf("JSON: %v (%s)", err, out)
			}
			if len(result.Data) != 1 || result.Data[0]["appKey"] != tc.appKey || result.Data[0]["prompt"] != nil || result.Data[0]["plugins"] != nil {
				t.Errorf("unexpected summaries: %+v", result.Data)
			}
			if p := result.Pagination; p.Count != 1 || p.Page != 2 || p.Size != 10 || p.Total != 21 {
				t.Errorf("unexpected pagination: %+v", p)
			}
		})
	}
	child, _, err := agentCmd.Find([]string{"list"})
	if err != nil || child == agentCmd || child.Name() != "list" {
		t.Fatalf("real agent parent must register list: %v", err)
	}
}

func TestAgentListEmpty(t *testing.T) {
	for _, output := range []string{outputJSON, outputTable} {
		t.Run(output, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct{ Pagination struct{ Page, Size int } }
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.Pagination.Page != 1 || body.Pagination.Size != 20 {
					t.Errorf("unexpected defaults: %+v", body)
				}
				_, _ = w.Write([]byte(`{"code":200,"data":[],"pagination":{"page":1,"size":20,"total":0}}`))
			}))
			defer srv.Close()
			root := agentCreateTestRoot(t, srv.URL)
			root.SetArgs([]string{"agent", "list", "--app", "expense_app", "--output", output})
			out := captureStdout(t, func() {
				if err := root.Execute(); err != nil {
					t.Error(err)
				}
			})
			if output == outputJSON && (!strings.Contains(out, `"data": []`) || !strings.Contains(out, `"count": 0`)) {
				t.Errorf("empty JSON must contain an array: %s", out)
			}
			if output == outputTable && out != "No agents found in app 'expense_app'.\n" {
				t.Errorf("unexpected empty table output: %s", out)
			}
		})
	}
}

func TestAgentListInvalidInput(t *testing.T) {
	for _, args := range [][]string{
		{}, {"--app", ""}, {"--app", " "}, {"--app", strings.Repeat("a", 129)},
		{"--app", "demo", "extra"}, {"--app", "demo", "--page", "0"},
		{"--app", "demo", "--size", "0"}, {"--app", "demo", "--size", "101"},
		{"--app", "demo", "--output", "yaml"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
			defer srv.Close()
			root := agentCreateTestRoot(t, srv.URL)
			root.SetArgs(append([]string{"agent", "list"}, args...))
			if err := root.Execute(); err == nil {
				t.Error("expected invalid input error")
			}
			if calls != 0 {
				t.Errorf("invalid input made %d calls", calls)
			}
		})
	}
}

func TestAgentListAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":403,"msg":"forbidden","data":{"reason":"FORBIDDEN"}}`))
	}))
	defer srv.Close()
	root := agentCreateTestRoot(t, srv.URL)
	root.SetArgs([]string{"agent", "list", "--app", "demo", "--output", "json"})
	out := captureStdout(t, func() {
		if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "forbidden") {
			t.Errorf("unexpected error: %v", err)
		}
	})
	if out != "" {
		t.Errorf("failure must not print successful JSON: %s", out)
	}
}
