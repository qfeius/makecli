/**
 * [INPUT]: 依赖 variable.go、Client、debugSink 与错误哨兵，bytes、encoding/json、errors、httptest、http、strings、testing
 * [OUTPUT]: 验证变量 CRUD-L 的角色解析 AppKey、类型/scope/分页、空值、错误分类、损坏回执拒绝与 debug 密钥值脱敏
 * [POS]: internal/api 的变量协议测试，全部请求发往本地 httptest 服务
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListVariables(t *testing.T) {
	for _, role := range []string{RoleBeta, RoleProd} {
		for _, scope := range []string{"runtime", "automation"} {
			t.Run(role+"/"+scope, func(t *testing.T) {
				resolvedApp := "resolved_" + role
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodPost || r.URL.Path != variablePath || r.URL.Query().Get("appRole") != role || r.Header.Get("X-Make-Target") != "MakeService.ListResources" || r.Header.Get("Authorization") != "Bearer token" || r.Header.Get("Traceparent") == "" {
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
					if body.AppKey != "demo" || body.Filter.Expression != "properties.scope == '"+scope+"'" || body.Pagination.Page != 2 || body.Pagination.Size != 10 {
						t.Errorf("unexpected list body: %+v", body)
					}
					value := ""
					secret := "must-not-leak"
					variables := []Variable{
						{Key: "X", AppKey: resolvedApp, Type: "Make.Variable", Properties: VariableProperties{Scope: scope, Value: &value}},
						{Key: "TOKEN", AppKey: resolvedApp, Type: "Make.Secret", Properties: VariableProperties{Scope: scope, Value: &secret}},
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": variables, "pagination": map[string]int{"total": 15}})
				}))
				defer srv.Close()
				var debug bytes.Buffer
				client := New(srv.URL, "token", WithAppRole(role))
				client.debug = &debugSink{w: &debug, format: DebugJSON}
				variables, total, err := client.ListVariables("demo", scope, 2, 10)
				if err != nil || total != 15 || len(variables) != 2 {
					t.Fatalf("list = %+v, %d, %v", variables, total, err)
				}
				if variables[0].AppKey != resolvedApp || variables[0].Properties.Value == nil || *variables[0].Properties.Value != "" || variables[1].Properties.Value != nil {
					t.Fatal("plain empty value lost or secret value exposed")
				}
				if strings.Contains(debug.String(), "must-not-leak") || !strings.Contains(debug.String(), "properties.scope == '") || !strings.Contains(debug.String(), "TOKEN") || client.debug.hideBody || client.debug.redactVariables {
					t.Fatal("list debug must retain the request and metadata without leaking secret values or mutating the shared sink")
				}
			})
		}
	}
}

func TestListVariablesErrors(t *testing.T) {
	for _, response := range []string{
		`{"code":500,"msg":"unavailable","data":{}}`, `{"code":401,"msg":"expired"}`,
		`{"code":990300403,"msg":"expired"}`, `{`, `{"code":200,"data":{}}`,
		`{"code":200,"data":[{"key":"X","appKey":"other","type":"Make.Variable","properties":{"scope":"runtime"}}]}`,
		`{"code":200,"data":[{"key":"X","appKey":"demo","type":"Make.Variable","properties":{"scope":"automation"}}]}`,
		`{"code":200,"data":[{"key":"X","appKey":"demo","type":"Make.App","properties":{"scope":"runtime"}}]}`,
		`{"code":200,"data":[{"appKey":"demo","type":"Make.Variable","properties":{"scope":"runtime"}}]}`,
	} {
		t.Run(response, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(response)) }))
			defer srv.Close()
			variables, _, err := New(srv.URL, "token").ListVariables("demo", "runtime", 1, 20)
			if err == nil || variables != nil {
				t.Fatalf("expected rejected response, got %+v, %v", variables, err)
			}
			if strings.Contains(response, "expired") && !errors.Is(err, ErrAuthFailed) {
				t.Fatalf("authentication sentinel lost: %v", err)
			}
		})
	}
}

func TestGetVariableResolvedApp(t *testing.T) {
	for _, tc := range []struct {
		name, role, actual, resourceType string
		wantError                        bool
	}{
		{"direct", "", "demo", "Make.Variable", false},
		{"direct mismatch", "", "other", "Make.Variable", true},
		{"beta variable", RoleBeta, "actual_beta_app", "Make.Variable", false},
		{"prod variable", RoleProd, "actual_prod_app", "Make.Variable", false},
		{"beta secret", RoleBeta, "actual_beta_app", "Make.Secret", false},
		{"prod secret", RoleProd, "actual_prod_app", "Make.Secret", false},
		{"empty resolved key", RoleBeta, "", "Make.Variable", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body Variable
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body.AppKey != "demo" || body.Type != tc.resourceType || r.URL.Query().Get("appRole") != tc.role {
					t.Error("must send the original app key and role")
				}
				variable := Variable{Key: "X", AppKey: tc.actual, Type: tc.resourceType, Properties: VariableProperties{Scope: "runtime"}}
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": variable})
			}))
			defer srv.Close()
			client := New(srv.URL, "token", WithAppRole(tc.role))
			get := client.GetVariable
			if tc.resourceType == "Make.Secret" {
				get = client.GetSecret
			}
			got, err := get("X", "demo", "runtime")
			if (err != nil) != tc.wantError || (err == nil && got.AppKey != tc.actual) {
				t.Fatalf("resolved app identity = %+v, %v", got, err)
			}
		})
	}
}

func TestListVariablesRejectsMixedResolvedApps(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":200,"data":[{"key":"X","appKey":"actual_beta","type":"Make.Variable","properties":{"scope":"runtime"}},{"key":"Y","appKey":"another_app","type":"Make.Variable","properties":{"scope":"runtime"}}]}`))
	}))
	defer srv.Close()
	if _, _, err := New(srv.URL, "token", WithAppRole(RoleBeta)).ListVariables("demo", "runtime", 1, 20); err == nil {
		t.Fatal("one page cannot mix resources from different resolved apps")
	}
}

func TestDeleteVariableRequests(t *testing.T) {
	for _, role := range []string{RoleBeta, RoleProd} {
		for _, scope := range []string{"runtime", "automation"} {
			t.Run(role+"/"+scope, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method != http.MethodPost || r.URL.Path != variablePath || r.URL.Query().Get("appRole") != role || r.Header.Get("X-Make-Target") != "MakeService.DeleteResource" {
						t.Errorf("unexpected delete routing: %s %s", r.Method, r.URL)
					}
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					props, _ := body["properties"].(map[string]any)
					if len(body) != 4 || body["key"] != "X" || body["type"] != "Make.Variable" || body["appKey"] != "demo" || len(props) != 1 || props["scope"] != scope {
						t.Errorf("unexpected delete identity: %+v", body)
					}
					_, _ = w.Write([]byte(`{"code":200,"data":{}}`))
				}))
				defer srv.Close()
				if err := New(srv.URL, "token", WithAppRole(role)).DeleteVariable("X", "demo", scope); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestVariableRequests(t *testing.T) {
	for _, role := range []string{RoleBeta, RoleProd} {
		for _, scope := range []string{"runtime", "automation"} {
			t.Run(role+"/"+scope, func(t *testing.T) {
				value := ""
				variable := Variable{Key: "_X", Type: "Make.Variable", AppKey: "demo", Properties: VariableProperties{Scope: scope, Value: &value, Description: "endpoint"}}
				var targets []string
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					target := r.Header.Get("X-Make-Target")
					targets = append(targets, target)
					if r.Method != http.MethodPost || r.URL.Path != variablePath || r.URL.Query().Get("appRole") != role {
						t.Errorf("unexpected request: %s %s", r.Method, r.URL)
					}
					if r.Header.Get("Authorization") != "Bearer token" || r.Header.Get("Traceparent") == "" {
						t.Error("missing authentication or trace headers")
					}
					var body map[string]any
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					props, _ := body["properties"].(map[string]any)
					if body["key"] != "_X" || body["appKey"] != "demo" || props["scope"] != scope {
						t.Errorf("unexpected identity: %v", body)
					}
					if target == "MakeService.GetResource" {
						if len(body) != 4 || body["type"] != "Make.Variable" || len(props) != 1 {
							t.Errorf("get must send typed identity: %v", body)
						}
						_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": variable})
						return
					}
					meta, _ := body["meta"].(map[string]any)
					if body["type"] != "Make.Variable" || meta["version"] != "1.0.0" || props["value"] != "" || props["description"] != "endpoint" {
						t.Errorf("unexpected write: %v", body)
					}
					_, _ = w.Write([]byte(`{"code":200,"data":{}}`))
				}))
				defer srv.Close()
				client := New(srv.URL, "token", WithAppRole(role))
				got, err := client.GetVariable("_X", "demo", scope)
				if err != nil || got.Properties.Value == nil || *got.Properties.Value != "" {
					t.Fatalf("GetVariable = %+v, %v", got, err)
				}
				if err := client.CreateVariable(variable); err != nil {
					t.Fatal(err)
				}
				if err := client.UpdateVariable(variable); err != nil {
					t.Fatal(err)
				}
				if strings.Join(targets, ",") != "MakeService.GetResource,MakeService.CreateResource,MakeService.UpdateResource" {
					t.Errorf("unexpected actions: %v", targets)
				}
			})
		}
	}
}

func TestGetVariableErrors(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		want           error
	}{
		{"not found", `{"code":404,"msg":"missing","data":{}}`, ErrNotFound},
		{"auth", `{"code":990300403,"msg":"expired"}`, ErrAuthFailed},
		{"unauthorized", `{"code":401,"msg":"expired"}`, ErrAuthFailed},
		{"forbidden", `{"code":403,"msg":"denied","data":false}`, nil},
		{"server", `{"code":500,"msg":"unavailable","data":[]}`, nil},
		{"empty data", `{"code":200,"data":{}}`, nil},
		{"null data", `{"code":200,"data":null}`, nil},
		{"invalid JSON", `{`, nil},
		{"bad data", `{"code":200,"data":[]}`, nil},
		{"wrong scope", `{"code":200,"data":{"key":"X","appKey":"demo","type":"Make.Variable","properties":{"scope":"automation"}}}`, nil},
		{"wrong type", `{"code":200,"data":{"key":"X","appKey":"demo","type":"Make.App","properties":{"scope":"runtime"}}}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.response)) }))
			defer srv.Close()
			_, err := New(srv.URL, "token", WithAppRole(RoleBeta)).GetVariable("X", "demo", "runtime")
			if err == nil || errors.Is(err, ErrNotFound) != (tc.want == ErrNotFound) || errors.Is(err, ErrAuthFailed) != (tc.want == ErrAuthFailed) {
				t.Fatalf("unexpected error: %v, want %v", err, tc.want)
			}
		})
	}
}

func TestVariableWriteErrors(t *testing.T) {
	for _, action := range []string{"create", "update"} {
		for _, response := range []string{`{"code":409,"msg":"already exists","data":{}}`, `{"code":403,"msg":"denied"}`, `{"code":990300403,"msg":"expired"}`, `{`} {
			t.Run(action+response, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(response)) }))
				defer srv.Close()
				client := New(srv.URL, "token", WithAppRole(RoleBeta))
				write := client.CreateVariable
				if action == "update" {
					write = client.UpdateVariable
				}
				if err := write(Variable{}); err == nil {
					t.Fatal("expected write error")
				}
			})
		}
	}
	client := New("://invalid", "token")
	if _, err := client.GetVariable("X", "demo", "runtime"); err == nil {
		t.Fatal("expected transport error")
	}
	if err := client.CreateVariable(Variable{}); err == nil {
		t.Fatal("expected transport error")
	}
}
