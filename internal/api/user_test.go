/**
 * [INPUT]: 依赖 internal/api 包内的 Client（包内白盒），encoding/json、errors、net/http、net/http/httptest、strings、testing
 * [OUTPUT]: 覆盖 Client.GetUserInfo 的单元测试
 * [POS]: internal/api user.go 的配套测试，用 httptest 隔离网络
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
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

func TestGetUserInfo(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet {
				t.Errorf("unexpected method: %s", r.Method)
			}
			if r.URL.Path != "/user/v1/info" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			if r.Header.Get("X-Make-Target") != "MakeService.GetResource" {
				t.Errorf("unexpected X-Make-Target: %s", r.Header.Get("X-Make-Target"))
			}
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Errorf("unexpected Authorization: %s", r.Header.Get("Authorization"))
			}
			if r.Header.Get("Content-Type") != "" {
				t.Errorf("GET should not carry Content-Type, got: %s", r.Header.Get("Content-Type"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "成功",
				"data": map[string]any{
					"id":   "1000000000000000001",
					"name": "test-user",
					"tenant": map[string]any{
						"id": "1000", "tenantName": "示例租户",
					},
					"valid": true,
				},
			})
		}))
		defer srv.Close()

		info, err := New(srv.URL, "test-token").GetUserInfo()
		if err != nil {
			t.Fatalf("GetUserInfo: %v", err)
		}
		if info.ID != "1000000000000000001" || info.Name != "test-user" {
			t.Errorf("unexpected user: %+v", info)
		}
		if info.Tenant.ID != "1000" || info.Tenant.TenantName != "示例租户" {
			t.Errorf("unexpected tenant: %+v", info.Tenant)
		}
		if !info.Valid {
			t.Errorf("expected valid=true")
		}
	})

	t.Run("code 401 returns ErrAuthFailed", func(t *testing.T) {
		srv := newGetServer(t, http.StatusOK, `{"code":401,"msg":"未登录","data":null}`)
		defer srv.Close()

		_, err := New(srv.URL, "expired-token").GetUserInfo()
		if !errors.Is(err, ErrAuthFailed) {
			t.Fatalf("expected ErrAuthFailed, got: %v", err)
		}
	})

	t.Run("meta auth-failed code returns ErrAuthFailed", func(t *testing.T) {
		srv := newGetServer(t, http.StatusOK, `{"code":990300403,"msg":"token验证失败"}`)
		defer srv.Close()

		_, err := New(srv.URL, "bad-token").GetUserInfo()
		if !errors.Is(err, ErrAuthFailed) {
			t.Fatalf("expected ErrAuthFailed, got: %v", err)
		}
	})

	t.Run("non-200 business code returns error", func(t *testing.T) {
		srv := newGetServer(t, http.StatusOK, `{"code":400,"msg":"X-Make-Target不支持","data":null}`)
		defer srv.Close()

		_, err := New(srv.URL, "test-token").GetUserInfo()
		if err == nil || !strings.Contains(err.Error(), "API 错误 [400]") {
			t.Fatalf("expected API error, got: %v", err)
		}
	})

	t.Run("transport error", func(t *testing.T) {
		srv := httptest.NewServer(nil)
		srv.Close() // 先关掉制造连接失败

		_, err := New(srv.URL, "test-token").GetUserInfo()
		if err == nil {
			t.Fatal("expected transport error")
		}
	})
}
