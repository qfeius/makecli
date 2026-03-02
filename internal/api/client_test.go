/**
 * [INPUT]: 依赖 internal/api 包内的 Client（包内白盒），encoding/json、net/http、net/http/httptest
 * [OUTPUT]: 覆盖 Client.CreateApp 的单元测试（成功、API 错误、响应格式错误）
 * [POS]: internal/api client.go 的配套测试，用 httptest 隔离网络
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCreateApp(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Errorf("expected POST, got %s", r.Method)
			}
			if r.Header.Get("X-Make-Target") != "MakeService.CreateResource" {
				t.Errorf("unexpected X-Make-Target: %s", r.Header.Get("X-Make-Target"))
			}
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Errorf("unexpected Authorization: %s", r.Header.Get("Authorization"))
			}
			json.NewEncoder(w).Encode(apiResponse{Code: 200, Message: "create app success"})
		}))
		defer srv.Close()

		if err := New(srv.URL, "test-token").CreateApp("myapp"); err != nil {
			t.Fatalf("CreateApp: %v", err)
		}
	})

	t.Run("API error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(apiResponse{Code: 400, Message: "invalid name"})
		}))
		defer srv.Close()

		if err := New(srv.URL, "test-token").CreateApp("myapp"); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("invalid response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Write([]byte("not json"))
		}))
		defer srv.Close()

		if err := New(srv.URL, "test-token").CreateApp("myapp"); err == nil {
			t.Fatal("expected error for invalid JSON response")
		}
	})
}
