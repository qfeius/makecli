/**
 * [INPUT]: 依赖 agent_plugin.go、Client，errors、net/http、net/http/httptest、strings、testing
 * [OUTPUT]: 覆盖目录成功/空数组、鉴权、业务错误、非法响应及完整 configSchema
 * [POS]: internal/api 的 Agent plugin 列表协议回归测试
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListAgentPlugins(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		auth             bool
	}{
		{"success", `{"code":200,"data":[{"key":"plugin","name":"插件","description":"描述","status":"available","capabilities":["tools"],"configSchema":{"type":"object","properties":{"secret":{"type":"string","writeOnly":true}},"additionalProperties":false}}],"pagination":{"total":3}}`, "", false},
		{"empty", `{"code":200,"data":[],"pagination":{"total":0}}`, "", false},
		{"null", `{"code":200,"data":null,"pagination":{"total":0}}`, "", false},
		{"auth", `{"code":401,"msg":"unauthorized","data":{}}`, "鉴权失败", true},
		{"gateway auth", `{"code":990300403,"msg":"expired"}`, "鉴权失败", true},
		{"forbidden", `{"code":403,"msg":"forbidden","data":{"reason":"FORBIDDEN"}}`, "forbidden", false},
		{"wrong data", `{"code":200,"data":{}}`, "插件列表响应", false},
		{"malformed", `{`, "响应", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) }))
			defer srv.Close()
			plugins, total, err := New(srv.URL, "test-token").ListAgentPlugins(1, 20)
			if tc.want == "" {
				if err != nil || plugins == nil {
					t.Fatalf("plugins = %+v, error = %v", plugins, err)
				}
				if tc.name == "success" {
					if len(plugins) != 1 || total != 3 || plugins[0].Key != "plugin" || plugins[0].Status != "available" || len(plugins[0].Capabilities) != 1 || plugins[0].ConfigSchema["additionalProperties"] != false {
						t.Errorf("unexpected plugins: %+v, total %d", plugins, total)
					}
				} else if len(plugins) != 0 || total != 0 {
					t.Errorf("unexpected empty response: %+v, total %d", plugins, total)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) || plugins != nil {
				t.Fatalf("plugins = %+v, error = %v, want %q", plugins, err, tc.want)
			}
			if tc.auth && !errors.Is(err, ErrAuthFailed) {
				t.Errorf("authentication error must use ErrAuthFailed: %v", err)
			}
		})
	}
}
