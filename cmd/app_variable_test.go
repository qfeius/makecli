/**
 * [INPUT]: 依赖 app_variable.go、newAppCmd、命令测试夹具、internal/api，encoding/json、net/http、net/http/httptest、strings、testing、github.com/spf13/cobra
 * [OUTPUT]: 验证真实命令的变量新增/更新、Get 类型声明、runtime 与应用环境定位、值保留、密钥保护、无效输入及失败不写入
 * [POS]: cmd/app variable add 的命令集成测试，复用临时应用清单，HTTP 完全隔离
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
	"github.com/spf13/cobra"
)

func appVariableTestCmd(t *testing.T, url string) *cobra.Command {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("MAKE_CLI_CONFIG_DIR", t.TempDir())
	t.Setenv(EnvAccessToken, "variable-token")
	setProfile(t, "default")
	setContextFlag(t, "dev")
	setMetaServerURLFlag(t, url)
	oldToken := AccessToken
	AccessToken = ""
	t.Cleanup(func() { AccessToken = oldToken })
	cmd := newAppCmd()
	cmd.SilenceErrors, cmd.SilenceUsage = true, true
	return cmd
}

func TestAppVariableAdd(t *testing.T) {
	for _, tc := range []struct {
		name, key, value, output, env string
		exists                        bool
	}{
		{"create defaults", "BASE_URL", "https://api.example.com", outputJSON, "", false},
		{"update production", "_X", "  new value\nline two  ", outputTable, "production", true},
		{"create empty", "X", "", outputAuto, "beta", false},
		{"update long key", strings.Repeat("x", 128), "$(literal) `text`", outputJSON, "beta", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enterAppDir(t, "demo")
			stubStdoutTerminal(t, false)
			env, role, action := api.EnvBeta, api.RoleBeta, "created"
			if tc.env == api.EnvProduction {
				env, role = api.EnvProduction, api.RoleProd
			}
			writeTarget := "MakeService.CreateResource"
			if tc.exists {
				writeTarget, action = "MakeService.UpdateResource", "updated"
			}
			var targets []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				target := r.Header.Get("X-Make-Target")
				targets = append(targets, target)
				if r.Method != http.MethodPost || r.URL.Path != "/api/make/secret/v1/variable" || r.URL.Query().Get("appRole") != role {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				if r.Header.Get("Authorization") != "Bearer variable-token" {
					t.Error("must use the shared profile authentication")
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				props, _ := body["properties"].(map[string]any)
				if body["type"] != "Make.Variable" {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"code":400,"msg":"invalid request: invalid type","data":{}}`))
					return
				}
				if body["key"] != tc.key || body["appKey"] != "demo" || props["scope"] != "runtime" {
					t.Errorf("unexpected identity: %v", body)
				}
				if target == "MakeService.GetResource" {
					if !tc.exists {
						_, _ = w.Write([]byte(`{"code":404,"msg":"missing"}`))
						return
					}
					oldValue := "old"
					variable := api.Variable{Key: tc.key, Type: "Make.Variable", AppKey: "demo", Properties: api.VariableProperties{Scope: "runtime", Value: &oldValue, Description: "keep this"}}
					_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": variable})
					return
				}
				if target != writeTarget || body["type"] != "Make.Variable" || props["value"] != tc.value {
					t.Errorf("unexpected write: %s %+v", target, body)
				}
				if tc.exists && props["description"] != "keep this" {
					t.Error("update must preserve the existing description")
				}
				_, _ = w.Write([]byte(`{"code":200,"data":{}}`))
			}))
			defer srv.Close()
			cmd := appVariableTestCmd(t, srv.URL)
			args := []string{"variable", "add", tc.key, tc.value, "--output", tc.output}
			if tc.env != "" {
				args = append(args, "--env", tc.env)
			}
			cmd.SetArgs(args)
			out := captureStdout(t, func() {
				if err := cmd.Execute(); err != nil {
					t.Fatal(err)
				}
			})
			if strings.Join(targets, ",") != "MakeService.GetResource,"+writeTarget {
				t.Fatalf("unexpected actions: %v", targets)
			}
			if tc.output == outputTable {
				if !strings.Contains(out, "updated successfully") || !strings.Contains(out, "production") || !strings.Contains(out, "next deployment") {
					t.Errorf("unexpected table output: %s", out)
				}
				return
			}
			var receipt appVariableReceipt
			if err := json.Unmarshal([]byte(out), &receipt); err != nil {
				t.Fatal(err)
			}
			if receipt.AppKey != "demo" || receipt.Key != tc.key || receipt.Scope != "runtime" || receipt.Environment != env || receipt.Action != action || !strings.Contains(receipt.Message, "next deployment") {
				t.Errorf("unexpected JSON receipt: %+v", receipt)
			}
		})
	}
}

func TestAppVariableAddInvalidInput(t *testing.T) {
	for _, args := range [][]string{
		{}, {"X"}, {"X", "value", "extra"}, {"1X", "value"}, {"A-B", "value"},
		{"", "value"}, {strings.Repeat("A", 129), "value"}, {"MAKE_TOKEN", "value"},
		{"X", strings.Repeat("a", 32*1024+1)}, {"X", "value", "--env", "prod"},
		{"X", "value", "--env", "all"}, {"X", "value", "--output", "yaml"},
		{"X", "value", "--scope", "automation"},
	} {
		name := strings.Join(args, "/")
		t.Run(name[:min(len(name), 80)], func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
			defer srv.Close()
			cmd := appVariableTestCmd(t, srv.URL)
			cmd.SetArgs(append([]string{"variable", "add"}, args...))
			if err := cmd.Execute(); err == nil {
				t.Fatal("expected invalid input error")
			}
			if calls != 0 {
				t.Fatalf("invalid input made %d remote calls", calls)
			}
		})
	}
}

func TestAppVariableAddWriteFailure(t *testing.T) {
	for _, exists := range []bool{false, true} {
		t.Run(map[bool]string{false: "create", true: "update"}[exists], func(t *testing.T) {
			enterAppDir(t, "demo")
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("X-Make-Target") == "MakeService.GetResource" {
					if !exists {
						_, _ = w.Write([]byte(`{"code":404,"msg":"missing"}`))
						return
					}
					_, _ = w.Write([]byte(`{"code":200,"data":{"key":"X","type":"Make.Variable","appKey":"demo","properties":{"scope":"runtime","value":"old"}}}`))
					return
				}
				_, _ = w.Write([]byte(`{"code":409,"msg":"write conflict","data":{}}`))
			}))
			defer srv.Close()
			cmd := appVariableTestCmd(t, srv.URL)
			cmd.SetArgs([]string{"variable", "add", "X", "value", "--output", "json"})
			out := captureStdout(t, func() {
				if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "write conflict") {
					t.Errorf("expected write conflict, got %v", err)
				}
			})
			if calls != 2 || out != "" {
				t.Fatalf("calls=%d, stdout=%q", calls, out)
			}
		})
	}
}

func TestAppVariableAddReadFailuresDoNotWrite(t *testing.T) {
	for _, response := range []string{
		`{"code":200,"data":{"key":"X","type":"Make.Secret","appKey":"demo","properties":{"scope":"runtime"}}}`,
		`{"code":500,"msg":"unavailable","data":{}}`, `{"code":403,"msg":"denied"}`,
		`{"code":990300403,"msg":"expired"}`, `{"code":200,"data":{}}`, `{`,
	} {
		t.Run(response, func(t *testing.T) {
			enterAppDir(t, "demo")
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("X-Make-Target") != "MakeService.GetResource" {
					t.Error("read error or existing secret must not trigger a write")
				}
				_, _ = w.Write([]byte(response))
			}))
			defer srv.Close()
			cmd := appVariableTestCmd(t, srv.URL)
			cmd.SetArgs([]string{"variable", "add", "X", "value"})
			out := captureStdout(t, func() {
				if err := cmd.Execute(); err == nil {
					t.Fatal("expected failure")
				}
			})
			if calls != 1 || out != "" {
				t.Fatalf("calls=%d, stdout=%q", calls, out)
			}
		})
	}
}

func TestAppVariableAddRequiresProjectAndCredentials(t *testing.T) {
	for _, scenario := range []string{"no manifest", "invalid app key", "no credentials"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
			defer srv.Close()
			cmd := appVariableTestCmd(t, srv.URL)
			switch scenario {
			case "no manifest":
				chdir(t, t.TempDir())
			case "invalid app key":
				enterAppDir(t, "_bad")
			case "no credentials":
				enterAppDir(t, "demo")
				t.Setenv(EnvAccessToken, "")
			}
			cmd.SetArgs([]string{"variable", "add", "X", "value"})
			if err := cmd.Execute(); err == nil {
				t.Fatal("expected local prerequisite error")
			}
			if calls != 0 {
				t.Fatal("local prerequisite failure must not make remote requests")
			}
		})
	}
}
