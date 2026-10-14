/**
 * [INPUT]: 依赖 agent_runtime.go、newAgentCmd、agentCreateTestRoot、captureStdout、internal/api，encoding/json、net/http、net/http/httptest、strings、testing
 * [OUTPUT]: 覆盖运行服务目录路由、分页、凭证、输出、空列表及输入校验
 * [POS]: cmd/agent runtime list 的命令集成测试，httptest 隔离真实目录服务
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

func TestAgentRuntimeList(t *testing.T) {
	for _, output := range []string{outputTable, outputJSON, outputAuto} {
		t.Run(output, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/api/make/agent/v1/runtime" || r.Header.Get("X-Make-Target") != "MakeService.ListResources" || r.Header.Get("Authorization") != "Bearer override-token" {
					t.Errorf("unexpected routing or credentials: %s %s", r.Method, r.URL)
				}
				var body map[string]map[string]int
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if len(body) != 1 || body["pagination"]["page"] != 2 || body["pagination"]["size"] != 10 {
					t.Errorf("request must only contain pagination: %+v", body)
				}
				_, _ = w.Write([]byte(`{"code":200,"data":[{"key":"make-runtime-service","name":"共享服务","scope":"shared","meta":{"online":true}},{"key":"private-runtime","name":"专属服务","scope":"dedicated","meta":{"online":false}}],"pagination":{"page":2,"size":10,"total":12}}`))
			}))
			defer srv.Close()
			root := agentCreateTestRoot(t, srv.URL)
			parent, _, _ := root.Find([]string{"agent"})
			parent.AddCommand(newAgentRuntimeCmd())
			root.SetArgs([]string{"agent", "runtime", "list", "--page", "2", "--size", "10", "--output", output, "--access-token", "override-token"})
			out := captureStdout(t, func() {
				if err := root.Execute(); err != nil {
					t.Fatal(err)
				}
			})
			if calls != 1 {
				t.Fatalf("calls = %d, want 1", calls)
			}
			if output == outputTable {
				for _, want := range []string{"KEY", "NAME", "SCOPE", "ONLINE", "make-runtime-service", "shared", "dedicated", "true", "false", "Showing 2 of 12 runtimes"} {
					if !strings.Contains(out, want) {
						t.Errorf("missing %q in %s", want, out)
					}
				}
				return
			}
			var result struct {
				Data       []api.AgentRuntime
				Pagination struct{ Count, Page, Size, Total int }
			}
			if err := json.Unmarshal([]byte(out), &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Data) != 2 || !result.Data[0].Meta.Online || result.Data[1].Meta.Online || !strings.Contains(out, `"online": false`) || result.Pagination.Count != 2 || result.Pagination.Page != 2 || result.Pagination.Size != 10 || result.Pagination.Total != 12 {
				t.Errorf("unexpected JSON: %s", out)
			}
		})
	}
	cmd, _, err := newAgentCmd().Find([]string{"runtime", "list"})
	if err != nil || cmd.Name() != "list" || cmd.Parent().Name() != "runtime" {
		t.Fatalf("missing runtime list registration: %v", err)
	}
}

func TestAgentRuntimeListEmpty(t *testing.T) {
	for _, output := range []string{outputTable, outputJSON} {
		t.Run(output, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body struct{ Pagination struct{ Page, Size int } }
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.Pagination.Page != 1 || body.Pagination.Size != 20 {
					t.Errorf("unexpected defaults: %+v", body)
				}
				_, _ = w.Write([]byte(`{"code":200,"data":[],"pagination":{"total":0}}`))
			}))
			defer srv.Close()
			agentCreateTestRoot(t, srv.URL)
			out := captureStdout(t, func() {
				if err := runAgentRuntimeList(1, 20, output); err != nil {
					t.Fatal(err)
				}
			})
			if output == outputTable && out != "No agent runtimes found.\n" {
				t.Errorf("unexpected empty text: %s", out)
			}
			if output == outputJSON && !strings.Contains(out, `"data": []`) {
				t.Errorf("empty JSON must contain array: %s", out)
			}
		})
	}
}

func TestAgentRuntimeListInvalidInput(t *testing.T) {
	for _, args := range [][]string{
		{"--page", "0"}, {"--size", "0"}, {"--size", "101"}, {"--output", "yaml"}, {"extra"}, {"--app", "demo"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
			defer srv.Close()
			root := agentCreateTestRoot(t, srv.URL)
			parent, _, _ := root.Find([]string{"agent"})
			parent.AddCommand(newAgentRuntimeCmd())
			root.SetArgs(append([]string{"agent", "runtime", "list"}, args...))
			if err := root.Execute(); err == nil {
				t.Error("expected input error")
			}
			if calls != 0 {
				t.Errorf("invalid input made %d requests", calls)
			}
		})
	}
}

func TestAgentRuntimeListFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":403,"msg":"forbidden","data":{"reason":"FORBIDDEN"}}`))
	}))
	defer srv.Close()
	agentCreateTestRoot(t, srv.URL)
	out := captureStdout(t, func() {
		if err := runAgentRuntimeList(1, 20, outputJSON); err == nil || !strings.Contains(err.Error(), "forbidden") {
			t.Errorf("expected forbidden, got %v", err)
		}
	})
	if out != "" {
		t.Errorf("failed request printed success: %s", out)
	}
}
