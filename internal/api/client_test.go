/**
 * [INPUT]: 依赖 internal/api 包内的 Client（包内白盒），encoding/json、net/http、net/http/httptest
 * [OUTPUT]: 覆盖 Client.CreateApp / ListApps / DeleteApp 的单元测试
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
			if r.Header.Get("X-Make-Target") != "MakeService.CreateResource" {
				t.Errorf("unexpected X-Make-Target: %s", r.Header.Get("X-Make-Target"))
			}
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Errorf("unexpected Authorization: %s", r.Header.Get("Authorization"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "message": "create app success"})
		}))
		defer srv.Close()

		if err := New(srv.URL, "test-token").CreateApp("myapp"); err != nil {
			t.Fatalf("CreateApp: %v", err)
		}
	})

	t.Run("API error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 400, "message": "invalid name"})
		}))
		defer srv.Close()

		if err := New(srv.URL, "test-token").CreateApp("myapp"); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("invalid response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("not json"))
		}))
		defer srv.Close()

		if err := New(srv.URL, "test-token").CreateApp("myapp"); err == nil {
			t.Fatal("expected error for invalid JSON response")
		}
	})
}

func TestDeleteApp(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Make-Target") != "MakeService.DeleteResource" {
				t.Errorf("unexpected X-Make-Target: %s", r.Header.Get("X-Make-Target"))
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["name"] != "myapp" || body["type"] != "Make.App" {
				t.Errorf("unexpected body: %v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "delete app success"})
		}))
		defer srv.Close()

		if err := New(srv.URL, "test-token").DeleteApp("myapp"); err != nil {
			t.Fatalf("DeleteApp: %v", err)
		}
	})

	t.Run("API error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 500, "msg": "internal error"})
		}))
		defer srv.Close()

		if err := New(srv.URL, "test-token").DeleteApp("myapp"); err == nil {
			t.Fatal("expected error on API failure")
		}
	})
}

func TestListApps(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Make-Target") != "MakeService.ListResources" {
				t.Errorf("unexpected X-Make-Target: %s", r.Header.Get("X-Make-Target"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":    200,
				"message": "success",
				"data": []map[string]any{
					{"name": "项目A", "type": "Make.App", "meta": map[string]any{"version": "1.0.0"}, "properties": map[string]any{"code": "ProjectA"}},
					{"name": "项目B", "type": "Make.App", "meta": map[string]any{"version": "2.0.0"}, "properties": map[string]any{"code": "ProjectB"}},
				},
				"pagination": map[string]any{"page": 1, "size": 10, "total": 2},
			})
		}))
		defer srv.Close()

		apps, total, err := New(srv.URL, "test-token").ListApps(1, 10)
		if err != nil {
			t.Fatalf("ListApps: %v", err)
		}
		if total != 2 {
			t.Errorf("expected total=2, got %d", total)
		}
		if len(apps) != 2 {
			t.Errorf("expected 2 apps, got %d", len(apps))
		}
		if apps[0].Name != "项目A" {
			t.Errorf("unexpected first app name: %s", apps[0].Name)
		}
	})

	t.Run("API error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 500, "message": "internal error"})
		}))
		defer srv.Close()

		if _, _, err := New(srv.URL, "test-token").ListApps(1, 10); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("empty list", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "message": "success",
				"data":       []any{},
				"pagination": map[string]any{"page": 1, "size": 10, "total": 0},
			})
		}))
		defer srv.Close()

		apps, total, err := New(srv.URL, "test-token").ListApps(1, 10)
		if err != nil {
			t.Fatalf("ListApps: %v", err)
		}
		if total != 0 || len(apps) != 0 {
			t.Errorf("expected empty result, got apps=%d total=%d", len(apps), total)
		}
	})
}
