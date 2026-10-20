/**
 * [INPUT]: 依赖 app_variable.go、newAppCmd、命令测试夹具、internal/api，bytes、encoding/json、errors、net/http、httptest、os、strings、testing、cobra
 * [OUTPUT]: 验证变量 CRUD-L、put 仅更新/保留说明/不存在不创建、角色/properties.scope 筛选/分页/输出、空列表、类型保护与删除确认门禁
 * [POS]: cmd/app variable 的命令集成测试，复用临时应用清单，HTTP 完全隔离
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
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

func TestAppVariableList(t *testing.T) {
	for _, tc := range []struct {
		name, output, appRole string
		terminal              bool
	}{
		{"json defaults", outputJSON, "", false},
		{"table production", outputTable, "production", false},
		{"auto pipe", outputAuto, "beta", false},
		{"auto terminal", outputAuto, "beta", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enterAppDir(t, "demo")
			stubStdoutTerminal(t, tc.terminal)
			calls := 0
			role := api.RoleBeta
			if tc.appRole == api.EnvProduction {
				role = api.RoleProd
			}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/api/make/secret/v1/variable" || r.URL.Query().Get("appRole") != role || r.Header.Get("X-Make-Target") != "MakeService.ListResources" || r.Header.Get("Authorization") != "Bearer variable-token" {
					t.Errorf("unexpected list request: %s %s", r.Method, r.URL)
				}
				var body struct {
					AppKey     string
					Filter     struct{ Expression string }
					Pagination struct{ Page, Size int }
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.AppKey != "demo" || body.Filter.Expression != "properties.scope == 'runtime'" || body.Pagination.Page != 2 || body.Pagination.Size != 5 {
					t.Errorf("unexpected list body: %+v", body)
				}
				_, _ = w.Write([]byte(`{"code":200,"data":[{"key":"BASE_URL","type":"Make.Variable","appKey":"demo","meta":{"version":"1.0.0"},"properties":{"scope":"runtime","value":"https://api.example.com","description":"endpoint"}},{"key":"API_KEY","type":"Make.Secret","appKey":"demo","properties":{"scope":"runtime","value":"must-not-leak"}}],"pagination":{"total":12}}`))
			}))
			defer srv.Close()
			cmd := appVariableTestCmd(t, srv.URL)
			args := []string{"variable", "list", "--page", "2", "--size", "5", "--output", tc.output}
			if tc.appRole != "" {
				args = append(args, "--appRole", tc.appRole)
			}
			cmd.SetArgs(args)
			out := captureStdout(t, func() {
				if err := cmd.Execute(); err != nil {
					t.Fatal(err)
				}
			})
			if calls != 1 || strings.Contains(out, "must-not-leak") {
				t.Fatalf("calls=%d; leaked secret=%v", calls, strings.Contains(out, "must-not-leak"))
			}
			if tc.output == outputTable || tc.terminal {
				for _, want := range []string{"BASE_URL", "API_KEY", "(encrypted)", "runtime", "https://api.example.com", "Showing 2 of 12 variables"} {
					if !strings.Contains(out, want) {
						t.Errorf("table missing %q: %s", want, out)
					}
				}
				return
			}
			var result struct {
				Data       []api.Variable
				Pagination struct{ Count, Page, Size, Total int }
			}
			if err := json.Unmarshal([]byte(out), &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Data) != 2 || result.Data[1].Properties.Value != nil || result.Data[0].Meta["version"] != "1.0.0" || result.Pagination.Count != 2 || result.Pagination.Page != 2 || result.Pagination.Size != 5 || result.Pagination.Total != 12 {
				t.Fatalf("unexpected list JSON: %s", out)
			}
		})
	}
}

func TestAppConfigListEmpty(t *testing.T) {
	for _, output := range []string{outputJSON, outputTable} {
		for _, resource := range []string{"variable", "secret"} {
			t.Run(resource+"/"+output, func(t *testing.T) {
				enterAppDir(t, "demo")
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte(`{"code":200,"data":[],"pagination":{"total":0}}`))
				}))
				defer srv.Close()
				cmd := appVariableTestCmd(t, srv.URL)
				cmd.SetArgs([]string{resource, "list", "--output", output})
				out := captureStdout(t, func() {
					if err := cmd.Execute(); err != nil {
						t.Fatal(err)
					}
				})
				want := "No runtime " + resource + "s found"
				if output == outputJSON {
					want = `"data": []`
				}
				if !strings.Contains(out, want) {
					t.Fatalf("unexpected empty output: %s", out)
				}
			})
		}
	}
}

func TestAppVariableDelete(t *testing.T) {
	for _, tc := range []struct {
		name, output, appRole, yes string
		cancel                     bool
	}{
		{"confirmed", outputTable, "beta", "", false},
		{"cancelled", outputJSON, "production", "", true},
		{"long flag", outputJSON, "production", "--yes", false},
		{"short flag", outputAuto, "beta", "-y", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enterAppDir(t, "demo")
			stubStdoutTerminal(t, false)
			manifest, err := os.ReadFile(appDSLPath)
			if err != nil {
				t.Fatal(err)
			}
			calls, confirms := 0, 0
			cancelled := errors.New("deletion cancelled")
			original := confirmAppVariableDeleteFunc
			confirmAppVariableDeleteFunc = func(key, appKey, role string) error {
				confirms++
				if key != "BASE_URL" || appKey != "demo" || role != tc.appRole {
					t.Error("confirmation target does not match selected variable")
				}
				if tc.cancel {
					return cancelled
				}
				return nil
			}
			t.Cleanup(func() { confirmAppVariableDeleteFunc = original })
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Query().Get("appRole") != api.RoleForEnv(tc.appRole) || r.URL.Path != "/api/make/secret/v1/variable" || r.Header.Get("Authorization") != "Bearer variable-token" {
					t.Error("wrong deletion app role, path or credentials")
				}
				var body api.Variable
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.Key != "BASE_URL" || body.AppKey != "demo" || body.Type != "Make.Variable" || body.Properties.Scope != "runtime" || body.Properties.Value != nil {
					t.Errorf("unexpected delete identity: %+v", body)
				}
				target := r.Header.Get("X-Make-Target")
				if calls != 1 || target != "MakeService.DeleteResource" {
					t.Errorf("unexpected deletion sequence: %d %s", calls, target)
				}
				_, _ = w.Write([]byte(`{"code":200,"data":{}}`))
			}))
			defer srv.Close()
			cmd := appVariableTestCmd(t, srv.URL)
			args := []string{"variable", "delete", "BASE_URL", "--appRole", tc.appRole, "--output", tc.output}
			if tc.yes != "" {
				args = append(args, tc.yes)
			}
			cmd.SetArgs(args)
			out := captureStdout(t, func() {
				err := cmd.Execute()
				if tc.cancel {
					if !errors.Is(err, cancelled) {
						t.Errorf("expected cancellation, got %v", err)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
			})
			wantConfirms := 1
			if tc.yes != "" {
				wantConfirms = 0
			}
			if confirms != wantConfirms {
				t.Fatalf("confirmations=%d, want %d", confirms, wantConfirms)
			}
			if tc.cancel {
				if calls != 0 || out != "" {
					t.Fatal("cancelled deletion must not request or print success")
				}
				return
			}
			if calls != 1 || !strings.Contains(out, "next deployment") {
				t.Fatalf("calls=%d, output=%s", calls, out)
			}
			if tc.output != outputTable {
				var receipt appVariableReceipt
				if err := json.Unmarshal([]byte(out), &receipt); err != nil || receipt.Action != "deleted" || receipt.Key != "BASE_URL" || receipt.AppRole != tc.appRole || receipt.Scope != "runtime" || receipt.AppKey != "demo" {
					t.Fatalf("unexpected deletion receipt: %+v, %v", receipt, err)
				}
			}
			after, err := os.ReadFile(appDSLPath)
			if err != nil || !bytes.Equal(after, manifest) {
				t.Fatal("deletion must preserve app.yaml")
			}
		})
	}
}

func TestAppVariableManageInvalidInput(t *testing.T) {
	for _, args := range [][]string{
		{"list", "extra"}, {"list", "--page", "0"}, {"list", "--size", "0"}, {"list", "--size", "101"},
		{"list", "--appRole", "prod"}, {"list", "--output", "yaml"}, {"list", "--scope", "automation"},
		{"delete"}, {"delete", "X", "extra"}, {"delete", "1X"}, {"delete", "MAKE_X"},
		{"delete", "X", "--appRole", "prod"}, {"delete", "X", "--output", "yaml"},
	} {
		t.Run(strings.Join(args, "/"), func(t *testing.T) {
			enterAppDir(t, "demo")
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
			defer srv.Close()
			cmd := appVariableTestCmd(t, srv.URL)
			cmd.SetArgs(append([]string{"variable"}, args...))
			if err := cmd.Execute(); err == nil {
				t.Fatal("expected input error")
			}
			if calls != 0 {
				t.Fatal("invalid input made remote requests")
			}
		})
	}
}

func TestAppVariableManageFailures(t *testing.T) {
	for _, tc := range []struct{ name, command, response, want string }{
		{"secret protection", "delete", `{"code":409,"msg":"type is immutable"}`, "type is immutable"},
		{"missing variable", "delete", `{"code":404,"msg":"missing"}`, "missing"},
		{"delete unavailable", "delete", `{"code":500,"msg":"unavailable"}`, "unavailable"},
		{"delete error", "delete", `{"code":403,"msg":"delete denied"}`, "delete denied"},
		{"list error", "list", `{"code":500,"msg":"unavailable","data":{}}`, "unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enterAppDir(t, "demo")
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				_, _ = w.Write([]byte(tc.response))
			}))
			defer srv.Close()
			cmd := appVariableTestCmd(t, srv.URL)
			args := []string{"variable", tc.command, "--output", "json"}
			if tc.command == "delete" {
				args = append(args, "X", "--yes")
			}
			cmd.SetArgs(args)
			out := captureStdout(t, func() {
				if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Errorf("expected %q, got %v", tc.want, err)
				}
			})
			if calls != 1 || out != "" {
				t.Fatalf("calls=%d, stdout=%q", calls, out)
			}
		})
	}
}

func TestAppVariableDeleteNonInteractive(t *testing.T) {
	enterAppDir(t, "demo")
	stdin, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close() })
	original := os.Stdin
	os.Stdin = stdin
	t.Cleanup(func() { os.Stdin = original })
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer srv.Close()
	cmd := appVariableTestCmd(t, srv.URL)
	cmd.SetArgs([]string{"variable", "delete", "X"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected non-interactive refusal, got %v", err)
	}
	if calls != 0 {
		t.Fatal("unconfirmed deletion must make no requests")
	}
}

func TestAppVariableWrite(t *testing.T) {
	for _, tc := range []struct {
		name, verb, key, value, output, env string
		exists                              bool
	}{
		{"create defaults", "add", "BASE_URL", "https://api.example.com", outputJSON, "", false},
		{"update production", "add", "_X", "  new value\nline two  ", outputTable, "production", true},
		{"create empty", "add", "X", "", outputAuto, "beta", false},
		{"update long key", "add", strings.Repeat("x", 128), "$(literal) `text`", outputJSON, "beta", true},
		{"put defaults", "put", "BASE_URL", "https://api-v2.example.com", outputJSON, "", true},
		{"put production", "put", "_X", "  new value\nline two  ", outputTable, "production", true},
		{"put empty", "put", "X", "", outputAuto, "beta", true},
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
			args := []string{"variable", tc.verb, tc.key, tc.value, "--output", tc.output}
			if tc.env != "" {
				args = append(args, "--appRole", tc.env)
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
			if receipt.AppKey != "demo" || receipt.Key != tc.key || receipt.Scope != "runtime" || receipt.AppRole != env || receipt.Action != action || !strings.Contains(receipt.Message, "next deployment") {
				t.Errorf("unexpected JSON receipt: %+v", receipt)
			}
		})
	}
}

func TestAppVariableAddInvalidInput(t *testing.T) {
	for _, args := range [][]string{
		{}, {"X"}, {"X", "value", "extra"}, {"1X", "value"}, {"A-B", "value"},
		{"", "value"}, {strings.Repeat("A", 129), "value"}, {"MAKE_TOKEN", "value"},
		{"X", strings.Repeat("a", 32*1024+1)}, {"X", "value", "--appRole", "prod"},
		{"X", "value", "--appRole", "all"}, {"X", "value", "--output", "yaml"},
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

func TestAppVariableWriteFailure(t *testing.T) {
	for _, tc := range []struct {
		verb   string
		exists bool
	}{{"add", false}, {"add", true}, {"put", true}} {
		t.Run(tc.verb+map[bool]string{false: "/create", true: "/update"}[tc.exists], func(t *testing.T) {
			enterAppDir(t, "demo")
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("X-Make-Target") == "MakeService.GetResource" {
					if !tc.exists {
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
			cmd.SetArgs([]string{"variable", tc.verb, "X", "value", "--output", "json"})
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

func TestAppVariableReadFailuresDoNotWrite(t *testing.T) {
	for _, response := range []string{
		`{"code":200,"data":{"key":"X","type":"Make.Secret","appKey":"demo","properties":{"scope":"runtime"}}}`,
		`{"code":500,"msg":"unavailable","data":{}}`, `{"code":403,"msg":"denied"}`,
		`{"code":990300403,"msg":"expired"}`, `{"code":200,"data":{}}`, `{`,
	} {
		for _, verb := range []string{"add", "put"} {
			t.Run(verb+response, func(t *testing.T) {
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
				cmd.SetArgs([]string{"variable", verb, "X", "value"})
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
}

func TestAppVariablePutMissingDoesNotCreate(t *testing.T) {
	enterAppDir(t, "demo")
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("X-Make-Target") != "MakeService.GetResource" {
			t.Error("put must not create a missing variable")
		}
		_, _ = w.Write([]byte(`{"code":404,"msg":"variable not found"}`))
	}))
	defer srv.Close()
	cmd := appVariableTestCmd(t, srv.URL)
	cmd.SetArgs([]string{"variable", "put", "X", "value", "--output", "json"})
	out := captureStdout(t, func() {
		if err := cmd.Execute(); !errors.Is(err, api.ErrNotFound) {
			t.Fatalf("expected not-found error, got %v", err)
		}
	})
	if calls != 1 || out != "" {
		t.Fatalf("calls=%d, stdout=%q", calls, out)
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
