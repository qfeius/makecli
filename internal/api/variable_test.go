/**
 * [INPUT]: 依赖 variable.go、Client 与错误哨兵，encoding/json、errors、net/http/httptest、net/http、strings、testing
 * [OUTPUT]: 验证变量读写请求及 Get 必填 type、环境与 scope 隔离、空值、错误分类及损坏回执拒绝
 * [POS]: internal/api 的变量协议测试，全部请求发往本地 httptest 服务
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
