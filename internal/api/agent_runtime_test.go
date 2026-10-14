/**
 * [INPUT]: 依赖 agent_runtime.go、Client，errors、net/http、net/http/httptest、strings、testing
 * [OUTPUT]: 覆盖目录成功/空数组、鉴权、业务错误、非法响应及在线布尔值
 * [POS]: internal/api 的 Agent runtime 列表协议回归测试
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

func TestListAgentRuntimes(t *testing.T) {
	for _, tc := range []struct {
		name, body, want string
		auth             bool
	}{
		{"success", `{"code":200,"data":[{"key":"runtime","name":"运行服务","scope":"dedicated","meta":{"online":false}}],"pagination":{"total":3}}`, "", false},
		{"empty", `{"code":200,"data":[],"pagination":{"total":0}}`, "", false},
		{"null", `{"code":200,"data":null,"pagination":{"total":0}}`, "", false},
		{"auth", `{"code":401,"msg":"unauthorized","data":{}}`, "鉴权失败", true},
		{"gateway auth", `{"code":990300403,"msg":"expired"}`, "鉴权失败", true},
		{"forbidden", `{"code":403,"msg":"forbidden","data":{"reason":"FORBIDDEN"}}`, "forbidden", false},
		{"wrong data", `{"code":200,"data":{}}`, "运行服务列表响应", false},
		{"malformed", `{`, "响应", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) }))
			defer srv.Close()
			runtimes, total, err := New(srv.URL, "test-token").ListAgentRuntimes(1, 20)
			if tc.want == "" {
				if err != nil || runtimes == nil {
					t.Fatalf("runtimes = %+v, error = %v", runtimes, err)
				}
				if tc.name == "success" {
					if len(runtimes) != 1 || total != 3 || runtimes[0].Key != "runtime" || runtimes[0].Scope != "dedicated" || runtimes[0].Meta.Online {
						t.Errorf("unexpected runtimes: %+v, total %d", runtimes, total)
					}
				} else if len(runtimes) != 0 || total != 0 {
					t.Errorf("unexpected empty response: %+v, total %d", runtimes, total)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) || runtimes != nil {
				t.Fatalf("runtimes = %+v, error = %v, want %q", runtimes, err, tc.want)
			}
			if tc.auth && !errors.Is(err, ErrAuthFailed) {
				t.Errorf("authentication error must use ErrAuthFailed: %v", err)
			}
		})
	}
}
