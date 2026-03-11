/**
 * [INPUT]: 依赖 cmd 包内的 runAppList（包内白盒），internal/config、encoding/json、net/http、net/http/httptest
 * [OUTPUT]: 覆盖 app list 子命令核心逻辑的单元测试
 * [POS]: cmd 模块 app_list.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunAppList(t *testing.T) {
	t.Run("lists apps successfully", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Make-Target") != "MakeService.ListResources" {
				t.Errorf("unexpected X-Make-Target: %s", r.Header.Get("X-Make-Target"))
			}
			var req struct {
				Pagination struct {
					Page int `json:"page"`
					Size int `json:"size"`
				} `json:"pagination"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if req.Pagination.Page != 1 {
				t.Errorf("unexpected pagination page: %d", req.Pagination.Page)
			}
			if req.Pagination.Size != 20 {
				t.Errorf("unexpected pagination size: %d", req.Pagination.Size)
			}
			json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "message": "success",
				"data": []map[string]any{
					{"name": "项目A", "type": "Make.App",
						"meta":       map[string]any{"version": "1.0.0"},
						"properties": map[string]any{"code": "ProjectA"}},
				},
				"pagination": map[string]any{"page": 1, "size": 20, "total": 1},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		if err := runAppList("default", srv.URL, 1, 20, outputTable); err != nil {
			t.Fatalf("runAppList: %v", err)
		}
	})

	t.Run("empty list prints message", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "message": "success",
				"data":       []any{},
				"pagination": map[string]any{"page": 1, "size": 20, "total": 0},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		if err := runAppList("default", srv.URL, 1, 20, outputTable); err != nil {
			t.Fatalf("runAppList: %v", err)
		}
	})

	t.Run("prints json when requested", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "message": "success",
				"data": []map[string]any{
					{"name": "项目A", "type": "Make.App",
						"meta":       map[string]any{"version": "1.0.0"},
						"properties": map[string]any{"code": "ProjectA"}},
				},
				"pagination": map[string]any{"page": 1, "size": 20, "total": 1},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		output := captureStdout(t, func() {
			if err := runAppList("default", srv.URL, 2, 20, outputJSON); err != nil {
				t.Fatalf("runAppList json: %v", err)
			}
		})

		if !strings.Contains(output, "\"data\"") {
			t.Fatalf("expected JSON output, got %q", output)
		}
		if !strings.Contains(output, "\"count\": 1") {
			t.Fatalf("expected pagination count in JSON output, got %q", output)
		}
		if !strings.Contains(output, "\"page\": 2") {
			t.Fatalf("expected pagination page in JSON output, got %q", output)
		}
		if strings.Contains(output, "Showing 1 of 1 apps") {
			t.Fatalf("expected JSON-only output, got %q", output)
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		if err := runAppList("default", "http://localhost", 1, 20, outputTable); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails on API error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"code": 500, "message": "server error"})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		if err := runAppList("default", srv.URL, 1, 20, outputTable); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails when page is less than 1", func(t *testing.T) {
		if err := runAppList("default", "http://localhost", 0, 20, outputTable); err == nil {
			t.Fatal("expected error for invalid page")
		}
	})

	t.Run("fails on unsupported output format", func(t *testing.T) {
		if err := runAppList("default", "http://localhost", 1, 20, "xml"); err == nil {
			t.Fatal("expected error for unsupported output format")
		}
	})
}
