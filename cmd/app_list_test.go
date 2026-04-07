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
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "message": "success",
				"data": []map[string]any{
					{"name": "项目A", "type": "Make.App",
						"meta":       map[string]any{"version": "1.0.0", "createdAt": "2026-04-03T04:44:23Z"},
						"properties": map[string]any{"renderName": "ProjectA"}},
				},
				"pagination": map[string]any{"page": 1, "size": 20, "total": 1},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		if err := runAppList("default", 1, 20, outputTable, ""); err != nil {
			t.Fatalf("runAppList: %v", err)
		}
	})

	t.Run("empty list prints message", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "message": "success",
				"data":       []any{},
				"pagination": map[string]any{"page": 1, "size": 20, "total": 0},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		if err := runAppList("default", 1, 20, outputTable, ""); err != nil {
			t.Fatalf("runAppList: %v", err)
		}
	})

	t.Run("prints json when requested", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "message": "success",
				"data": []map[string]any{
					{"name": "项目A", "type": "Make.App",
						"meta":       map[string]any{"version": "1.0.0", "createdAt": "2026-04-03T04:44:23Z"},
						"properties": map[string]any{"renderName": "ProjectA"}},
				},
				"pagination": map[string]any{"page": 1, "size": 20, "total": 1},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		output := captureStdout(t, func() {
			if err := runAppList("default", 2, 20, outputJSON, ""); err != nil {
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
		ServerURL = "http://unused"
		if err := runAppList("default", 1, 20, outputTable, ""); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails on API error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 500, "message": "server error"})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		if err := runAppList("default", 1, 20, outputTable, ""); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails when page is less than 1", func(t *testing.T) {
		if err := runAppList("default", 0, 20, outputTable, ""); err == nil {
			t.Fatal("expected error for invalid page")
		}
	})

	t.Run("fails when size is less than 1", func(t *testing.T) {
		if err := runAppList("default", 1, 0, outputTable, ""); err == nil {
			t.Fatal("expected error for invalid size")
		}
	})

	t.Run("fails on unsupported output format", func(t *testing.T) {
		if err := runAppList("default", 1, 20, "xml", ""); err == nil {
			t.Fatal("expected error for unsupported output format")
		}
	})

	t.Run("sends filter to API", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			f, ok := req["filter"].(map[string]any)
			if !ok {
				t.Fatal("expected filter in request body")
			}
			nameFilter, ok := f["name"].(map[string]any)
			if !ok {
				t.Fatal("expected name filter to be object with contains")
			}
			if nameFilter["contains"] != "项目" {
				t.Errorf("expected contains=项目, got %v", nameFilter["contains"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "message": "success",
				"data":       []any{},
				"pagination": map[string]any{"page": 1, "size": 20, "total": 0},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		if err := runAppList("default", 1, 20, outputTable, "name=项目"); err != nil {
			t.Fatalf("runAppList with filter: %v", err)
		}
	})

	t.Run("fails on invalid filter expression", func(t *testing.T) {
		if err := runAppList("default", 1, 20, outputTable, "badfilter"); err == nil {
			t.Fatal("expected error for invalid filter")
		}
	})

	t.Run("fails on unsupported filter field", func(t *testing.T) {
		if err := runAppList("default", 1, 20, outputTable, "status=active"); err == nil {
			t.Fatal("expected error for unsupported filter field")
		}
	})
}

func TestParseFilter(t *testing.T) {
	t.Run("empty returns nil", func(t *testing.T) {
		f, err := parseFilter("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f != nil {
			t.Fatalf("expected nil, got %v", f)
		}
	})

	t.Run("name becomes contains", func(t *testing.T) {
		f, err := parseFilter("name=项目")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		nameObj, ok := f["name"].(map[string]any)
		if !ok {
			t.Fatalf("expected name to be map, got %T", f["name"])
		}
		if nameObj["contains"] != "项目" {
			t.Errorf("expected contains=项目, got %v", nameObj["contains"])
		}
	})

	t.Run("rejects missing value", func(t *testing.T) {
		if _, err := parseFilter("name="); err == nil {
			t.Fatal("expected error for missing value")
		}
	})

	t.Run("rejects missing key", func(t *testing.T) {
		if _, err := parseFilter("=value"); err == nil {
			t.Fatal("expected error for missing key")
		}
	})

	t.Run("rejects unknown field", func(t *testing.T) {
		if _, err := parseFilter("foo=bar"); err == nil {
			t.Fatal("expected error for unknown field")
		}
	})
}
