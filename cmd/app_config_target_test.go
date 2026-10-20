/**
 * [INPUT]: 依赖 app_variable/app_secret 命令、应用测试夹具、internal/api、httptest，encoding/json、net/http、os、strings、testing
 * [OUTPUT]: 验证全部配置命令的 --app 优先及 beta/prod 角色契约，GET 返回实际 AppKey 后仍以入口 key 更新，拒绝无效 key
 * [POS]: cmd 配置管理的应用目标集成测试；默认清单路径由现有变量/密钥测试覆盖
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/qfeius/makecli/internal/api"
)

func TestAppConfigExplicitApp(t *testing.T) {
	for _, group := range []string{"variable", "secret"} {
		for _, verb := range []string{"add", "put", "list", "delete"} {
			for _, project := range []string{"missing", "different", "broken"} {
				t.Run(group+"/"+verb+"/"+project, func(t *testing.T) {
					if project == "missing" {
						chdir(t, t.TempDir())
					} else {
						enterAppDir(t, "local_app")
					}
					if project == "broken" {
						if err := os.WriteFile(appDSLPath, []byte("invalid: ["), 0o600); err != nil {
							t.Fatal(err)
						}
					}
					typeName := "Make.Variable"
					if group == "secret" {
						typeName = "Make.Secret"
					}
					var targets []string
					srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						var body api.Variable
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Error(err)
						}
						if body.AppKey != "remote_app" || r.URL.Path != "/api/make/secret/v1/variable" || r.URL.Query().Get("appRole") != "prod" {
							t.Error("explicit app must be sent directly with the selected role")
						}
						target := r.Header.Get("X-Make-Target")
						targets = append(targets, target)
						variable := api.Variable{Key: "CONFIG_KEY", Type: typeName, AppKey: "resolved_app", Properties: api.VariableProperties{Scope: "runtime"}}
						switch target {
						case "MakeService.GetResource":
							if verb == "add" {
								_, _ = w.Write([]byte(`{"code":404,"msg":"missing"}`))
								return
							}
							_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": variable})
						case "MakeService.ListResources":
							_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": []api.Variable{variable}, "pagination": map[string]int{"total": 1}})
						default:
							if body.Type != typeName || body.Key != "CONFIG_KEY" || body.Properties.Scope != "runtime" {
								t.Error("configuration identity changed")
							}
							_, _ = w.Write([]byte(`{"code":200,"data":{}}`))
						}
					}))
					defer srv.Close()
					cmd := appVariableTestCmd(t, srv.URL)
					args := []string{group, verb}
					if verb != "list" {
						args = append(args, "CONFIG_KEY")
					}
					if verb == "add" || verb == "put" {
						if group == "secret" {
							args = append(args, "--from-stdin")
							cmd.SetIn(strings.NewReader("fake-secret-value"))
						} else {
							args = append(args, "plain-value")
						}
					}
					if verb == "delete" {
						args = append(args, "--yes")
					}
					cmd.SetArgs(append(args, "--app", "remote_app", "--appRole", "prod", "--output", "json"))
					out := captureStdout(t, func() {
						if err := cmd.Execute(); err != nil {
							t.Fatal(err)
						}
					})
					outputAppKey := "remote_app"
					if verb == "list" {
						outputAppKey = "resolved_app"
					}
					if !strings.Contains(out, outputAppKey) || strings.Contains(out, "fake-secret-value") {
						t.Fatal("receipt must use explicit app and omit secrets")
					}
					want := map[string]string{
						"add": "MakeService.GetResource,MakeService.CreateResource", "put": "MakeService.GetResource,MakeService.UpdateResource",
						"list": "MakeService.ListResources", "delete": "MakeService.DeleteResource",
					}[verb]
					if strings.Join(targets, ",") != want {
						t.Fatalf("unexpected requests: %v", targets)
					}
				})
			}
		}
	}
}

func TestAppConfigInvalidExplicitApp(t *testing.T) {
	for _, group := range []string{"variable", "secret"} {
		t.Run(group, func(t *testing.T) {
			enterAppDir(t, "local_app")
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
			defer srv.Close()
			cmd := appVariableTestCmd(t, srv.URL)
			cmd.SetArgs([]string{group, "list", "--app", "invalid/key"})
			if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "invalid key") || calls != 0 {
				t.Fatalf("invalid explicit app must not fall back to the valid manifest: calls=%d error=%v", calls, err)
			}
		})
	}
}

func TestAppConfigRejectsProductionRole(t *testing.T) {
	for _, args := range [][]string{
		{"variable", "add", "CONFIG_KEY", "value"}, {"variable", "put", "CONFIG_KEY", "value"},
		{"variable", "list"}, {"variable", "delete", "CONFIG_KEY", "--yes"},
		{"secret", "add", "CONFIG_KEY", "--from-stdin"}, {"secret", "put", "CONFIG_KEY", "--from-stdin"},
		{"secret", "list"}, {"secret", "delete", "CONFIG_KEY", "--yes"},
	} {
		t.Run(strings.Join(args[:2], "/"), func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
			defer srv.Close()
			cmd := appVariableTestCmd(t, srv.URL)
			cmd.SetArgs(append(args, "--app", "remote_app", "--appRole", "production"))
			if err := cmd.Execute(); err == nil || err.Error() != "--appRole must be beta or prod" || calls != 0 {
				t.Fatalf("expected beta/prod validation before HTTP: calls=%d error=%v", calls, err)
			}
		})
	}
}
