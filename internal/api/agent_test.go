/**
 * [INPUT]: 依赖 agent.go 和 Client，encoding/json、errors、net/http、net/http/httptest、strings、testing
 * [OUTPUT]: 覆盖 appKey 应用归属、Agent 创建、列表、详情与删除请求、公开配置与概要响应、可选字段及错误回执
 * [POS]: internal/api 的 Agent 创建、列表、详情与删除协议回归测试，httptest 隔离网络
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

func TestCreateAgent(t *testing.T) {
	disabled := false
	req := CreateAgentRequest{
		Key: "expense_agent", Name: "报销助手", AppKey: "expense_app",
		RuntimeKey: "make-runtime-service", ModelKey: "deepseek", Status: "disabled",
		Prompt: "按实际结果回答", Description: "报销",
		Plugins: []CreateAgentPlugin{{PluginKey: "mcp_make", Enabled: &disabled, Config: map[string]any{}}},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/agent/v1/agent" {
			t.Errorf("unexpected endpoint: %s %s", r.Method, r.URL)
		}
		if r.Header.Get("X-Make-Target") != "MakeService.CreateResource" || r.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("unexpected routing/auth headers")
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["appKey"] != "expense_app" || body["productKey"] != nil || body["runtimeKey"] != "make-runtime-service" || body["type"] != nil || body["properties"] != nil {
			t.Errorf("unexpected request body: %v", body)
		}
		plugins, ok := body["plugins"].([]any)
		if !ok || len(plugins) != 1 {
			t.Errorf("unexpected plugins: %v", body["plugins"])
		} else if plugins[0].(map[string]any)["enabled"] != false {
			t.Error("explicit false must be preserved")
		}
		_, _ = w.Write([]byte(`{"code":200,"msg":"success","data":{"key":"expense_agent","name":"报销助手","appKey":"expense_app","runtimeKey":"make-runtime-service","modelKey":"deepseek","status":"disabled","prompt":"按实际结果回答","plugins":[{"id":"binding_01","pluginKey":"mcp_make","enabled":false,"config":{},"channelEndpoint":{"serverURL":"https://example.com/callback"}}],"meta":{"createdAt":"2026-10-12T00:00:00Z","updatedAt":"2026-10-12T00:00:00Z"}}}`))
	}))
	defer srv.Close()
	saved, err := New(srv.URL, "test-token").CreateAgent(req)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Key != req.Key || saved.AppKey != req.AppKey || len(saved.Plugins) != 1 || saved.Plugins[0].ID != "binding_01" || saved.Plugins[0].ChannelEndpoint == nil || saved.Meta.CreatedAt == "" {
		t.Errorf("unexpected response: %+v", saved)
	}
}

func TestGetAgent(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		sentinel         error
	}{
		{"success", `{"code":200,"data":{"key":"poc_agent","appKey":"agent_poc","prompt":"hello","plugins":[{"id":"binding_01","pluginKey":"mcp_make","enabled":false,"config":{}}]}}`, "", nil},
		{"not found", `{"code":404,"msg":"missing","data":{"reason":"NOT_FOUND"}}`, "", ErrNotFound},
		{"empty", `{"code":200,"data":{}}`, "", ErrNotFound},
		{"auth", `{"code":401,"msg":"unauthorized","data":{}}`, "", ErrAuthFailed},
		{"forbidden", `{"code":403,"msg":"forbidden","data":{"reason":"FORBIDDEN"}}`, "forbidden", nil},
		{"wrong data", `{"code":200,"data":[]}`, "Agent 响应", nil},
		{"malformed", `{`, "响应", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/agent/v1/agent" || r.Header.Get("X-Make-Target") != "MakeService.GetResource" {
					t.Errorf("unexpected routing: %s %s", r.Method, r.URL)
				}
				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if len(body) != 2 || body["key"] != "poc_agent" || body["appKey"] != "agent_poc" {
					t.Errorf("unexpected body: %+v", body)
				}
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			agent, err := New(srv.URL, "test-token").GetAgent("poc_agent", "agent_poc")
			if tc.sentinel != nil {
				if !errors.Is(err, tc.sentinel) {
					t.Errorf("error = %v, want %v", err, tc.sentinel)
				}
				return
			}
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) || errors.Is(err, ErrNotFound) {
					t.Errorf("error = %v, want %q", err, tc.want)
				}
				return
			}
			if err != nil || agent == nil || agent.Prompt != "hello" || len(agent.Plugins) != 1 || agent.Plugins[0].ID != "binding_01" {
				t.Errorf("agent = %+v, err = %v", agent, err)
			}
		})
	}
}

func TestDeleteAgent(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{
		{"success", `{"code":200,"msg":"success","data":{}}`, "", 200},
		{"not found", `{"code":404,"msg":"agent not found","data":{"reason":"NOT_FOUND"}}`, "agent not found", 404},
		{"forbidden", `{"code":403,"msg":"forbidden","data":{"reason":"FORBIDDEN"}}`, "forbidden", 403},
		{"auth", `{"code":401,"msg":"unauthorized","data":{}}`, "鉴权失败", 200},
		{"gateway auth", `{"code":990300403,"msg":"expired"}`, "鉴权失败", 200},
		{"malformed", `{`, "响应", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/agent/v1/agent" || r.Header.Get("X-Make-Target") != "MakeService.DeleteResource" {
					t.Errorf("unexpected routing: %s %s", r.Method, r.URL)
				}
				var body map[string]string
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if len(body) != 2 || body["key"] != "invoice_agent" || body["appKey"] != "invoice_app" {
					t.Errorf("unexpected body: %+v", body)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			err := New(srv.URL, "test-token").DeleteAgent("invoice_agent", "invoice_app")
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want %q", err, tc.want)
			}
			if strings.Contains(tc.name, "auth") && !errors.Is(err, ErrAuthFailed) {
				t.Errorf("authentication error must use ErrAuthFailed: %v", err)
			}
		})
	}
}

func TestCreateAgentOptionalFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		for _, key := range []string{"description", "prompt", "runtimeKey", "status", "plugins"} {
			if _, exists := body[key]; exists {
				t.Errorf("optional %s should be omitted", key)
			}
		}
		_, _ = w.Write([]byte(`{"code":200,"msg":"success","data":{"key":"demo"}}`))
	}))
	defer srv.Close()
	_, err := New(srv.URL, "test-token").CreateAgent(CreateAgentRequest{Key: "demo", Name: "demo", AppKey: "expense_app", ModelKey: "deepseek"})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCreateAgentErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{
		{"conflict", `{"code":409,"msg":"Agent already exists","data":{"reason":"KEY_CONFLICT"}}`, "Agent already exists", 409},
		{"auth", `{"code":401,"msg":"unauthorized","data":{"reason":"UNAUTHORIZED"}}`, "鉴权失败", 401},
		{"forbidden", `{"code":403,"msg":"forbidden","data":{"reason":"FORBIDDEN"}}`, "forbidden", 403},
		{"bad json", `{`, "响应", 200},
		{"null data", `{"code":200,"data":null}`, "缺少 key", 200},
		{"wrong data", `{"code":200,"data":[]}`, "响应", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			_, err := New(srv.URL, "test-token").CreateAgent(CreateAgentRequest{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %s", err, tc.want)
			}
			if tc.name == "auth" && !errors.Is(err, ErrAuthFailed) {
				t.Errorf("401 must use ErrAuthFailed: %v", err)
			}
		})
	}
}

func TestListAgents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/agent/v1/agent" || r.Header.Get("X-Make-Target") != "MakeService.ListResources" {
			t.Errorf("unexpected list routing: %s %s", r.Method, r.URL)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["appKey"] != nil || body["filter"].(map[string]any)["expression"] != "appKey == 'expense_app'" {
			t.Errorf("unexpected filter: %v", body)
		}
		pagination := body["pagination"].(map[string]any)
		if pagination["page"] != float64(2) || pagination["size"] != float64(20) {
			t.Errorf("unexpected pagination: %v", pagination)
		}
		_, _ = w.Write([]byte(`{"code":200,"msg":"success","data":[{"key":"expense_agent","name":"报销助手","appKey":"expense_app","description":"报销查询","runtimeKey":"make-runtime-service","modelKey":"deepseek","status":"disabled","meta":{"createdAt":"2026-10-13T00:00:00Z","updatedAt":"2026-10-13T00:00:00Z"}}],"pagination":{"page":2,"size":20,"total":42}}`))
	}))
	defer srv.Close()
	agents, total, err := New(srv.URL, "test-token").ListAgents(2, 20, "appKey == 'expense_app'")
	if err != nil {
		t.Fatal(err)
	}
	if total != 42 || len(agents) != 1 || agents[0].AppKey != "expense_app" || agents[0].Meta.UpdatedAt == "" || agents[0].Description != "报销查询" {
		t.Errorf("unexpected summaries: %+v, total %d", agents, total)
	}
}

func TestListAgentsResponses(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{
		{"empty", `{"code":200,"data":[],"pagination":{"total":0}}`, "", 200},
		{"null", `{"code":200,"data":null,"pagination":{"total":0}}`, "", 200},
		{"business error", `{"code":400,"msg":"application scope required","data":{"reason":"INVALID_FILTER"}}`, "application scope required", 200},
		{"http error", `{"code":403,"msg":"forbidden","data":{"reason":"FORBIDDEN"}}`, "forbidden", 403},
		{"auth", `{"code":401,"msg":"unauthorized","data":{"reason":"UNAUTHORIZED"}}`, "鉴权失败", 200},
		{"malformed", `{`, "响应", 200},
		{"wrong data", `{"code":200,"data":{}}`, "Agent 列表响应", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			agents, total, err := New(srv.URL, "test-token").ListAgents(1, 20, "appKey == 'demo'")
			if tc.want == "" {
				if err != nil || agents == nil || len(agents) != 0 || total != 0 {
					t.Fatalf("empty list = %+v, %d, %v", agents, total, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %s", err, tc.want)
			}
			if tc.name == "auth" && !errors.Is(err, ErrAuthFailed) {
				t.Errorf("401 must use ErrAuthFailed: %v", err)
			}
		})
	}
}
