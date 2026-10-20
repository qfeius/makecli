/**
 * [INPUT]: 依赖 app_secret.go、真实 Cobra 命令与 httptest、应用测试夹具、internal/api，encoding/json、errors、io、net/http、os、strings、testing
 * [OUTPUT]: 验证密钥 CRUD-L、put 不创建/保留说明/错误脱敏、properties.scope/type 筛选分页/输出、角色隔离、stdin 边界与删除确认门禁
 * [POS]: cmd/app secret 的命令契约测试；密钥均为本地测试值，HTTP 不访问平台
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/qfeius/makecli/internal/api"
)

func TestAppSecretWrite(t *testing.T) {
	for _, tc := range []struct {
		name, verb, role, output, input, value string
		exists                                 bool
	}{
		{"create beta", "add", "beta", outputJSON, " test-secret-value\r\n", " test-secret-value", false},
		{"rotate production", "add", "production", outputTable, "test-secret-new\nline two  \n", "test-secret-new\nline two  ", true},
		{"empty", "add", "beta", outputJSON, "", "", false},
		{"put beta", "put", "beta", outputAuto, " test-secret-value\r\n", " test-secret-value", true},
		{"put production", "put", "production", outputTable, "test-secret-new\nline two  \n", "test-secret-new\nline two  ", true},
		{"put empty", "put", "beta", outputJSON, "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enterAppDir(t, "demo")
			stubStdoutTerminal(t, false)
			role, writeTarget, action := "beta", "MakeService.CreateResource", "created"
			if tc.role == "production" {
				role = "prod"
			}
			if tc.exists {
				writeTarget, action = "MakeService.UpdateResource", "updated"
			}
			var targets []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				target := r.Header.Get("X-Make-Target")
				targets = append(targets, target)
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				props, _ := body["properties"].(map[string]any)
				if r.Method != http.MethodPost || r.URL.Path != "/api/make/secret/v1/variable" || r.URL.Query().Get("appRole") != role ||
					body["type"] != "Make.Secret" || body["appKey"] != "demo" || body["key"] != "OPENAI_API_KEY" || props["scope"] != "runtime" {
					t.Error("incorrect secret request identity")
				}
				if target == "MakeService.GetResource" {
					if !tc.exists {
						_, _ = io.WriteString(w, `{"code":404,"msg":"missing"}`)
						return
					}
					_, _ = io.WriteString(w, `{"code":200,"data":{"key":"OPENAI_API_KEY","type":"Make.Secret","appKey":"demo","properties":{"scope":"runtime","description":"keep"}}}`)
					return
				}
				if target != writeTarget || props["value"] != tc.value {
					t.Error("incorrect secret write action or value")
				}
				if tc.exists && props["description"] != "keep" {
					t.Error("rotation must preserve description")
				}
				_, _ = io.WriteString(w, `{"code":200,"data":{}}`)
			}))
			defer srv.Close()
			cmd := appVariableTestCmd(t, srv.URL)
			cmd.SetIn(strings.NewReader(tc.input))
			args := []string{"secret", tc.verb, "OPENAI_API_KEY", "--from-stdin", "--output", tc.output}
			if tc.role != "beta" {
				args = append(args, "--appRole", tc.role)
			}
			cmd.SetArgs(args)
			out := captureStdout(t, func() {
				if err := cmd.Execute(); err != nil {
					t.Fatal(err)
				}
			})
			if strings.Join(targets, ",") != "MakeService.GetResource,"+writeTarget || strings.Contains(out, "test-secret-") {
				t.Fatal("incorrect request sequence or secret leaked in receipt")
			}
			if tc.output != outputTable {
				var receipt appVariableReceipt
				if err := json.Unmarshal([]byte(out), &receipt); err != nil {
					t.Fatal(err)
				}
				if receipt.Action != action || receipt.AppRole != tc.role || receipt.Key != "OPENAI_API_KEY" || receipt.Scope != "runtime" {
					t.Fatalf("incorrect receipt: %+v", receipt)
				}
			} else if !strings.Contains(out, "Secret 'OPENAI_API_KEY' updated successfully") {
				t.Fatalf("incorrect receipt: %s", out)
			}
		})
	}
}

func TestAppSecretFailuresDoNotWrite(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		args           []string
		wantCalls      int
	}{
		{"non tty", "", nil, 0},
		{"reject env", "", []string{"--env", "production"}, 0},
		{"reject role", "", []string{"--appRole", "prod"}, 0},
		{"reject value argument", "", []string{"plaintext-value"}, 0},
		{"variable exists", `{"code":200,"data":{"key":"X","type":"Make.Variable","appKey":"demo","properties":{"scope":"runtime"}}}`, []string{"--from-stdin"}, 1},
		{"forbidden", `{"code":403,"msg":"denied"}`, []string{"--from-stdin"}, 1},
		{"broken success", `{"code":200,"data":{}}`, []string{"--from-stdin"}, 1},
	} {
		for _, verb := range []string{"add", "put"} {
			t.Run(verb+"/"+tc.name, func(t *testing.T) {
				enterAppDir(t, "demo")
				calls := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					if r.Header.Get("X-Make-Target") != "MakeService.GetResource" {
						t.Error("must not write")
					}
					_, _ = io.WriteString(w, tc.response)
				}))
				defer srv.Close()
				cmd := appVariableTestCmd(t, srv.URL)
				cmd.SetIn(strings.NewReader("test-secret-value"))
				cmd.SetArgs(append([]string{"secret", verb, "X"}, tc.args...))
				out := captureStdout(t, func() {
					if err := cmd.Execute(); err == nil {
						t.Fatal("expected failure")
					} else if strings.Contains(err.Error(), "test-secret-value") {
						t.Fatal("secret leaked in error")
					}
				})
				if calls != tc.wantCalls || out != "" {
					t.Fatalf("calls=%d, stdout=%q", calls, out)
				}
			})
		}
	}
}

func TestAppSecretPutFailures(t *testing.T) {
	for _, writeFailure := range []bool{false, true} {
		t.Run(map[bool]string{false: "missing", true: "write failure"}[writeFailure], func(t *testing.T) {
			enterAppDir(t, "demo")
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("X-Make-Target") == "MakeService.GetResource" {
					if !writeFailure {
						_, _ = io.WriteString(w, `{"code":404,"msg":"missing"}`)
						return
					}
					_, _ = io.WriteString(w, `{"code":200,"data":{"key":"X","type":"Make.Secret","appKey":"demo","properties":{"scope":"runtime"}}}`)
					return
				}
				if r.Header.Get("X-Make-Target") != "MakeService.UpdateResource" {
					t.Error("put must never create a secret")
				}
				_, _ = io.WriteString(w, `{"code":409,"msg":"rejected test-secret-value"}`)
			}))
			defer srv.Close()
			cmd := appVariableTestCmd(t, srv.URL)
			cmd.SetIn(strings.NewReader("test-secret-value"))
			cmd.SetArgs([]string{"secret", "put", "X", "--from-stdin", "--output", "json"})
			out := captureStdout(t, func() {
				err := cmd.Execute()
				if err == nil || strings.Contains(err.Error(), "test-secret-value") {
					t.Fatal("put failure lost or exposed secret")
				}
				if !writeFailure && !errors.Is(err, api.ErrNotFound) {
					t.Fatalf("expected not found, got %v", err)
				}
				if writeFailure && !strings.Contains(err.Error(), "REDACTED") {
					t.Fatal("reflected secret must be redacted")
				}
			})
			wantCalls := 1
			if writeFailure {
				wantCalls = 2
			}
			if calls != wantCalls || out != "" {
				t.Fatalf("calls=%d, stdout=%q", calls, out)
			}
		})
	}
}

func TestAppSecretList(t *testing.T) {
	for _, tc := range []struct {
		output, role string
		terminal     bool
	}{
		{outputTable, "beta", false}, {outputJSON, "production", false},
		{outputAuto, "beta", false}, {outputAuto, "production", true},
	} {
		t.Run(tc.output+"/"+tc.role, func(t *testing.T) {
			enterAppDir(t, "demo")
			stubStdoutTerminal(t, tc.terminal)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("appRole") != api.RoleForEnv(tc.role) || r.Header.Get("X-Make-Target") != "MakeService.ListResources" || r.Header.Get("Authorization") != "Bearer variable-token" {
					t.Error("wrong secret list routing or credentials")
				}
				var body struct {
					AppKey     string
					Filter     struct{ Expression string }
					Pagination struct{ Page, Size int }
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.AppKey != "demo" || body.Filter.Expression != "properties.scope == 'runtime' && type == 'Make.Secret'" || body.Pagination.Page != 2 || body.Pagination.Size != 5 {
					t.Errorf("incorrect server-side secret pagination: %+v", body)
				}
				_, _ = io.WriteString(w, `{"code":200,"data":[{"key":"API_KEY","type":"Make.Secret","appKey":"demo","properties":{"scope":"runtime","value":"test-secret-must-not-leak","description":"credential"}}],"pagination":{"total":7}}`)
			}))
			defer srv.Close()
			cmd := appVariableTestCmd(t, srv.URL)
			cmd.SetArgs([]string{"secret", "list", "--page", "2", "--size", "5", "--appRole", tc.role, "--output", tc.output})
			out := captureStdout(t, func() {
				if err := cmd.Execute(); err != nil {
					t.Fatal(err)
				}
			})
			if strings.Contains(out, "test-secret-") {
				t.Fatal("secret value leaked in list")
			}
			if tc.output == outputTable || tc.terminal {
				if !strings.Contains(out, "(encrypted)") || !strings.Contains(out, "Showing 1 of 7 secrets") {
					t.Fatalf("incorrect table: %s", out)
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
			if len(result.Data) != 1 || result.Data[0].Type != "Make.Secret" || result.Data[0].Properties.Value != nil || result.Pagination.Count != 1 || result.Pagination.Page != 2 || result.Pagination.Size != 5 || result.Pagination.Total != 7 || strings.Contains(out, `"value"`) {
				t.Fatalf("incorrect secret metadata: %s", out)
			}
		})
	}
}

func TestAppSecretDelete(t *testing.T) {
	for _, tc := range []struct {
		name, role, output, yes string
		cancel                  bool
	}{
		{"confirmed", "beta", outputTable, "", false},
		{"cancelled", "production", outputJSON, "", true},
		{"yes", "production", outputJSON, "--yes", false},
		{"short yes", "beta", outputAuto, "-y", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enterAppDir(t, "demo")
			stubStdoutTerminal(t, false)
			calls, confirms := 0, 0
			cancelled := errors.New("deletion cancelled")
			original := confirmAppSecretDeleteFunc
			confirmAppSecretDeleteFunc = func(key, appKey, role string) error {
				confirms++
				if key != "API_KEY" || appKey != "demo" || role != tc.role {
					t.Error("incorrect secret deletion confirmation")
				}
				if tc.cancel {
					return cancelled
				}
				return nil
			}
			t.Cleanup(func() { confirmAppSecretDeleteFunc = original })
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodPost || r.URL.Path != "/api/make/secret/v1/variable" || r.URL.Query().Get("appRole") != api.RoleForEnv(tc.role) || r.Header.Get("X-Make-Target") != "MakeService.DeleteResource" {
					t.Error("wrong secret delete routing")
				}
				var body api.Variable
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.Type != "Make.Secret" || body.AppKey != "demo" || body.Key != "API_KEY" || body.Properties.Scope != "runtime" || body.Properties.Value != nil {
					t.Errorf("incorrect secret delete body: %+v", body)
				}
				_, _ = io.WriteString(w, `{"code":200,"data":{}}`)
			}))
			defer srv.Close()
			cmd := appVariableTestCmd(t, srv.URL)
			args := []string{"secret", "delete", "API_KEY", "--appRole", tc.role, "--output", tc.output}
			if tc.yes != "" {
				args = append(args, tc.yes)
			}
			cmd.SetArgs(args)
			out := captureStdout(t, func() {
				err := cmd.Execute()
				if tc.cancel {
					if !errors.Is(err, cancelled) {
						t.Errorf("cancellation lost: %v", err)
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
				t.Fatalf("calls=%d, stdout=%q", calls, out)
			}
			if tc.output != outputTable {
				var receipt appVariableReceipt
				if err := json.Unmarshal([]byte(out), &receipt); err != nil || receipt.Action != "deleted" || receipt.Key != "API_KEY" || receipt.AppKey != "demo" || receipt.AppRole != tc.role || receipt.Scope != "runtime" {
					t.Fatalf("incorrect secret deletion receipt: %+v, %v", receipt, err)
				}
			} else if !strings.Contains(out, "Secret 'API_KEY' deleted successfully") {
				t.Fatalf("incorrect text receipt: %s", out)
			}
		})
	}
}

func TestAppSecretManageInvalidInput(t *testing.T) {
	for _, args := range [][]string{
		{"put"}, {"put", "X", "value"}, {"put", "MAKE_X", "--from-stdin"},
		{"list", "extra"}, {"list", "--page", "0"}, {"list", "--size", "101"},
		{"list", "--appRole", "prod"}, {"list", "--output", "yaml"},
		{"delete"}, {"delete", "X", "extra"}, {"delete", "1X"},
		{"delete", "X", "--appRole", "prod"}, {"delete", "X", "--output", "yaml"},
	} {
		t.Run(strings.Join(args, "/"), func(t *testing.T) {
			enterAppDir(t, "demo")
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
			defer srv.Close()
			cmd := appVariableTestCmd(t, srv.URL)
			cmd.SetArgs(append([]string{"secret"}, args...))
			if err := cmd.Execute(); err == nil || calls != 0 {
				t.Fatalf("invalid input: calls=%d, error=%v", calls, err)
			}
		})
	}
}

func TestAppSecretDeleteNonInteractive(t *testing.T) {
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
	cmd.SetArgs([]string{"secret", "delete", "API_KEY"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--yes") || !strings.Contains(err.Error(), `secret "API_KEY"`) {
		t.Fatalf("expected non-interactive secret refusal, got %v", err)
	}
	if calls != 0 {
		t.Fatal("unconfirmed secret deletion must not make requests")
	}
}

func TestAppSecretManageFailures(t *testing.T) {
	for _, tc := range []struct{ name, command, response string }{
		{"missing", "delete", `{"code":404,"msg":"missing"}`},
		{"variable protection", "delete", `{"code":409,"msg":"type is immutable"}`},
		{"forbidden", "delete", `{"code":403,"msg":"denied"}`},
		{"list failure", "list", `{"code":500,"msg":"unavailable","data":{}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			enterAppDir(t, "demo")
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				_, _ = io.WriteString(w, tc.response)
			}))
			defer srv.Close()
			cmd := appVariableTestCmd(t, srv.URL)
			args := []string{"secret", tc.command, "--output", "json"}
			if tc.command == "delete" {
				args = append(args, "API_KEY", "--yes")
			}
			cmd.SetArgs(args)
			out := captureStdout(t, func() {
				if err := cmd.Execute(); err == nil {
					t.Fatal("expected backend failure")
				}
			})
			if calls != 1 || out != "" {
				t.Fatalf("calls=%d, stdout=%q", calls, out)
			}
		})
	}
}

func TestReadSecretStdin(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		wantError         bool
	}{
		{"spaces", "  value  ", "  value  ", false},
		{"one LF", "a\n\n", "a\n", false},
		{"CRLF", "a\r\n", "a", false},
		{"CR only", "a\r", "a\r", false},
		{"limit", strings.Repeat("x", 32*1024) + "\r\n", strings.Repeat("x", 32*1024), false},
		{"over limit", strings.Repeat("x", 32*1024+1), "", true},
		{"byte limit", strings.Repeat("密", 11000), "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := readSecretStdin(strings.NewReader(tc.input), "X")
			if (err != nil) != tc.wantError || got != tc.want {
				t.Fatal("stdin preservation or byte limit failed")
			}
		})
	}
}
