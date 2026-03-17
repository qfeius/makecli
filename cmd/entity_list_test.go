/**
 * [INPUT]: 依赖 cmd 包内的 runEntityList（包内白盒），internal/config、encoding/json、net/http、net/http/httptest
 * [OUTPUT]: 覆盖 entity list 子命令核心逻辑的单元测试（列表/空列表/具体entity/无凭证/API错误/未知profile）
 * [POS]: cmd 模块 entity_list.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
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

func TestRunEntityList(t *testing.T) {
	t.Run("lists entities successfully", func(t *testing.T) {
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
				"code": 200, "msg": "success",
				"data": []map[string]any{
					{"name": "项目", "type": "Make.Entity", "app": "TODO", "meta": map[string]any{"version": "1.0.0"}, "properties": map[string]any{"fields": []any{}}},
					{"name": "任务", "type": "Make.Entity", "app": "TODO", "meta": map[string]any{"version": "1.0.0"}, "properties": map[string]any{"fields": []any{}}},
				},
				"pagination": map[string]any{"page": 1, "size": 20, "total": 2},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		if err := runEntityList("TODO", "", "default", srv.URL, 1, 20, outputTable); err != nil {
			t.Fatalf("runEntityList: %v", err)
		}
	})

	t.Run("empty list prints message", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "success",
				"data":       []any{},
				"pagination": map[string]any{"page": 1, "size": 20, "total": 0},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		if err := runEntityList("TODO", "", "default", srv.URL, 1, 20, outputTable); err != nil {
			t.Fatalf("runEntityList empty: %v", err)
		}
	})

	t.Run("prints list as json when requested", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "success",
				"data": []map[string]any{
					{"name": "项目", "type": "Make.Entity", "app": "TODO", "meta": map[string]any{"version": "1.0.0"}, "properties": map[string]any{"fields": []any{}}},
				},
				"pagination": map[string]any{"page": 1, "size": 20, "total": 1},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		output := captureStdout(t, func() {
			if err := runEntityList("TODO", "", "default", srv.URL, 2, 20, outputJSON); err != nil {
				t.Fatalf("runEntityList json list: %v", err)
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
		if strings.Contains(output, "Showing 1 of 1 entities") {
			t.Fatalf("expected JSON-only output, got %q", output)
		}
	})

	t.Run("shows specific entity with fields", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Make-Target") != "MakeService.GetResource" {
				t.Errorf("unexpected X-Make-Target: %s", r.Header.Get("X-Make-Target"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "success",
				"data": map[string]any{
					"name": "项目", "type": "Make.Entity", "app": "TODO",
					"meta": map[string]any{"version": "1.0.0"},
					"properties": map[string]any{
						"fields": []map[string]any{
							{"name": "项目名称", "type": "Make.Field.Text", "meta": map[string]any{"version": "1.0.0"}, "properties": nil},
							{"name": "项目描述", "type": "Make.Field.TextArea", "meta": map[string]any{"version": "1.0.0"}, "properties": nil},
						},
					},
				},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		if err := runEntityList("TODO", "项目", "default", srv.URL, 1, 20, outputTable); err != nil {
			t.Fatalf("runEntityList with name: %v", err)
		}
	})

	t.Run("prints specific entity as json when requested", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "success",
				"data": map[string]any{
					"name": "项目", "type": "Make.Entity", "app": "TODO",
					"meta": map[string]any{"version": "1.0.0"},
					"properties": map[string]any{
						"fields": []map[string]any{
							{"name": "项目名称", "type": "Make.Field.Text", "meta": map[string]any{"version": "1.0.0"}, "properties": nil},
						},
					},
				},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		output := captureStdout(t, func() {
			if err := runEntityList("TODO", "项目", "default", srv.URL, 1, 20, outputJSON); err != nil {
				t.Fatalf("runEntityList json detail: %v", err)
			}
		})

		if !strings.Contains(output, "\"name\": \"项目\"") {
			t.Fatalf("expected entity name in JSON output, got %q", output)
		}
		if strings.Contains(output, "Fields:") {
			t.Fatalf("expected JSON-only output, got %q", output)
		}
	})

	t.Run("shows specific entity with no fields", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "success",
				"data": map[string]any{
					"name": "空实体", "type": "Make.Entity", "app": "TODO",
					"meta":       map[string]any{"version": "1.0.0"},
					"properties": map[string]any{"fields": []any{}},
				},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		if err := runEntityList("TODO", "空实体", "default", srv.URL, 1, 20, outputTable); err != nil {
			t.Fatalf("runEntityList no fields: %v", err)
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		if err := runEntityList("TODO", "", "default", "http://localhost", 1, 20, outputTable); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		if err := runEntityList("TODO", "", "nonexistent", "http://localhost", 1, 20, outputTable); err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})

	t.Run("fails on list API error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 500, "msg": "server error"})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		if err := runEntityList("TODO", "", "default", srv.URL, 1, 20, outputTable); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails on get API error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 404, "msg": "entity not found"})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		if err := runEntityList("TODO", "不存在", "default", srv.URL, 1, 20, outputTable); err == nil {
			t.Fatal("expected error on get API failure")
		}
	})

	t.Run("fails when page is less than 1", func(t *testing.T) {
		if err := runEntityList("TODO", "", "default", "http://localhost", 0, 20, outputTable); err == nil {
			t.Fatal("expected error for invalid page")
		}
	})

	t.Run("fails when size is less than 1", func(t *testing.T) {
		if err := runEntityList("TODO", "", "default", "http://localhost", 1, 0, outputTable); err == nil {
			t.Fatal("expected error for invalid size")
		}
	})

	t.Run("fails on unsupported output format", func(t *testing.T) {
		if err := runEntityList("TODO", "", "default", "http://localhost", 1, 20, "xml"); err == nil {
			t.Fatal("expected error for unsupported output format")
		}
	})
}
