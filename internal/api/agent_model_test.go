/**
 * [INPUT]: 依赖 agent_model.go、Client，errors、net/http、net/http/httptest、strings、testing
 * [OUTPUT]: 覆盖目录成功/空数组、鉴权、业务错误、非法响应、推理参数及多模态布尔值
 * [POS]: internal/api 的 Agent model 列表协议回归测试
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

func TestListAgentModels(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		auth             bool
	}{
		{"success", `{"code":200,"data":[{"key":"model","name":"模型","provider":"deepseek","model":"deepseek-test","reasoningEffort":"high","isMultiModal":false}],"pagination":{"total":3}}`, "", false},
		{"empty", `{"code":200,"data":[],"pagination":{"total":0}}`, "", false},
		{"null", `{"code":200,"data":null,"pagination":{"total":0}}`, "", false},
		{"auth", `{"code":401,"msg":"unauthorized","data":{}}`, "鉴权失败", true},
		{"gateway auth", `{"code":990300403,"msg":"expired"}`, "鉴权失败", true},
		{"forbidden", `{"code":403,"msg":"forbidden","data":{"reason":"FORBIDDEN"}}`, "forbidden", false},
		{"wrong data", `{"code":200,"data":{}}`, "模型列表响应", false},
		{"malformed", `{`, "响应", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) }))
			defer srv.Close()
			models, total, err := New(srv.URL, "test-token").ListAgentModels(1, 20)
			if tc.want == "" {
				if err != nil || models == nil {
					t.Fatalf("models = %+v, error = %v", models, err)
				}
				if tc.name == "success" {
					if len(models) != 1 || total != 3 || models[0].Key != "model" || models[0].Provider != "deepseek" || models[0].Model != "deepseek-test" || models[0].ReasoningEffort != "high" || models[0].IsMultiModal {
						t.Errorf("unexpected models: %+v, total %d", models, total)
					}
				} else if len(models) != 0 || total != 0 {
					t.Errorf("unexpected empty response: %+v, total %d", models, total)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) || models != nil {
				t.Fatalf("models = %+v, error = %v, want %q", models, err, tc.want)
			}
			if tc.auth && !errors.Is(err, ErrAuthFailed) {
				t.Errorf("authentication error must use ErrAuthFailed: %v", err)
			}
		})
	}
}
