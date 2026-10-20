/**
 * [INPUT]: 依赖 secret.go / variable.go、debugSink、httptest，bytes、encoding/json、errors、io、net/http、strings、testing
 * [OUTPUT]: 验证 Secret 类型 CRUD-L、properties.scope/type 筛选/分页、text/JSON 写入正文隐藏及列表安全展示、错误反射脱敏、错误链与错误类型响应拒绝
 * [POS]: internal/api 的密钥保密测试，不使用真实凭据
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSecretRequestsProtectValues(t *testing.T) {
	for _, format := range []DebugFormat{DebugText, DebugJSON} {
		for _, fail := range []bool{false, true} {
			t.Run(string(rune('0'+format))+map[bool]string{false: "/success", true: "/failure"}[fail], func(t *testing.T) {
				value := "test-secret-\"literal\"\nvalue"
				calls := 0
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					calls++
					var body Variable
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
						return
					}
					if body.Type != "Make.Secret" || body.Key != "X" || body.AppKey != "demo" || body.Properties.Scope != "runtime" {
						t.Error("incorrect typed secret identity")
					}
					if r.Header.Get("X-Make-Target") == "MakeService.GetResource" {
						_, _ = io.WriteString(w, `{"code":200,"data":{"key":"X","appKey":"demo","type":"Make.Secret","properties":{"scope":"runtime"}}}`)
						return
					}
					if body.Properties.Value == nil || *body.Properties.Value != value {
						t.Error("redaction must not alter actual payload")
					}
					code := 200
					if fail {
						code = 409
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "msg": "server reflected " + value, "data": map[string]any{"value": value}})
				}))
				defer srv.Close()
				var debug bytes.Buffer
				client := New(srv.URL, "test-token", WithAppRole(RoleBeta))
				client.debug = &debugSink{w: &debug, format: format}
				secret, err := client.GetSecret("X", "demo", "runtime")
				if err != nil {
					t.Fatal(err)
				}
				secret.Properties.Value = &value
				for _, write := range []func(Variable) error{client.CreateVariable, client.UpdateVariable} {
					err = write(*secret)
					if (err != nil) != fail {
						t.Fatalf("unexpected write result: %v", err)
					}
					if err != nil && (strings.Contains(err.Error(), "test-secret-") || !strings.Contains(err.Error(), "REDACTED")) {
						t.Fatal("secret reflected in error")
					}
				}
				if calls != 3 || strings.Contains(debug.String(), "test-secret-") || !strings.Contains(debug.String(), "redacted") {
					t.Fatal("debug body redaction failed")
				}
				if client.debug.hideBody {
					t.Fatal("secret calls must not mutate the shared debug sink")
				}
			})
		}
	}
}

func TestSecretErrorRetainsCause(t *testing.T) {
	value := "test-secret"
	err := redactSecretError(authFailedErr(401, value), &value)
	if !errors.Is(err, ErrAuthFailed) || strings.Contains(err.Error(), value) {
		t.Fatal("authentication sentinel or redaction lost")
	}
}

func TestSecretListAndDeleteRequests(t *testing.T) {
	for _, tc := range []struct {
		role, scope string
		format      DebugFormat
	}{
		{RoleBeta, "runtime", DebugText}, {RoleProd, "runtime", DebugJSON},
		{RoleBeta, "automation", DebugJSON}, {RoleProd, "automation", DebugText},
	} {
		t.Run(tc.role+"/"+tc.scope, func(t *testing.T) {
			var targets []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				target := r.Header.Get("X-Make-Target")
				targets = append(targets, target)
				if r.Method != http.MethodPost || r.URL.Path != variablePath || r.URL.Query().Get("appRole") != tc.role || r.Header.Get("Authorization") != "Bearer token" || r.Header.Get("Traceparent") == "" {
					t.Errorf("unexpected secret request routing: %s %s", r.Method, r.URL)
				}
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["appKey"] != "demo" {
					t.Error("incorrect secret app identity")
				}
				if target == "MakeService.ListResources" {
					filter, _ := body["filter"].(map[string]any)
					pagination, _ := body["pagination"].(map[string]any)
					if filter["expression"] != "properties.scope == '"+tc.scope+"' && type == 'Make.Secret'" || pagination["page"] != float64(2) || pagination["size"] != float64(5) {
						t.Errorf("incorrect secret server-side filter or page: %+v", body)
					}
					value := "must-not-leak"
					variable := Variable{Key: "TOKEN", Type: "Make.Secret", AppKey: "demo", Properties: VariableProperties{Scope: tc.scope, Value: &value}}
					_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": []Variable{variable}, "pagination": map[string]int{"total": 9}})
					return
				}
				props, _ := body["properties"].(map[string]any)
				if target != "MakeService.DeleteResource" || len(body) != 4 || body["key"] != "TOKEN" || body["type"] != "Make.Secret" || len(props) != 1 || props["scope"] != tc.scope {
					t.Errorf("incorrect typed secret deletion: %+v", body)
				}
				_, _ = io.WriteString(w, `{"code":200,"data":{"value":"must-not-leak"}}`)
			}))
			defer srv.Close()
			var debug bytes.Buffer
			client := New(srv.URL, "token", WithAppRole(tc.role))
			client.debug = &debugSink{w: &debug, format: tc.format}
			secrets, total, err := client.ListSecrets("demo", tc.scope, 2, 5)
			if err != nil || len(secrets) != 1 || total != 9 || secrets[0].Properties.Value != nil {
				t.Fatalf("incorrect secret page or exposed value: count=%d total=%d error=%v", len(secrets), total, err)
			}
			if err := client.DeleteSecret("TOKEN", "demo", tc.scope); err != nil {
				t.Fatal(err)
			}
			if strings.Join(targets, ",") != "MakeService.ListResources,MakeService.DeleteResource" {
				t.Fatalf("incorrect action sequence: %v", targets)
			}
			if strings.Contains(debug.String(), "must-not-leak") || !strings.Contains(debug.String(), "properties.scope == '") || !strings.Contains(debug.String(), "redacted") || client.debug.hideBody || client.debug.redactVariables {
				t.Fatal("secret debug must show the safe list and hide deletion bodies without changing the shared sink")
			}
		})
	}
}

func TestSecretListErrors(t *testing.T) {
	for _, response := range []string{
		`{"code":200,"data":[{"key":"X","type":"Make.Variable","appKey":"demo","properties":{"scope":"runtime","value":"ordinary-value"}}]}`,
		`{"code":401,"msg":"expired"}`, `{"code":990300403,"msg":"expired"}`,
		`{"code":500,"msg":"unavailable","data":{}}`, `{"code":200,"data":{}}`, `{`,
	} {
		t.Run(response, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, response)
			}))
			defer srv.Close()
			secrets, _, err := New(srv.URL, "token").ListSecrets("demo", "runtime", 1, 20)
			if err == nil || secrets != nil {
				t.Fatal("invalid secret list response must not return data")
			}
			if strings.Contains(response, "expired") && !errors.Is(err, ErrAuthFailed) {
				t.Fatalf("authentication sentinel lost: %v", err)
			}
		})
	}
}
