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
	"testing"
)

func TestRunEntityList(t *testing.T) {
	t.Run("lists entities successfully", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Make-Target") != "MakeService.ListResources" {
				t.Errorf("unexpected X-Make-Target: %s", r.Header.Get("X-Make-Target"))
			}
			json.NewEncoder(w).Encode(map[string]any{
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

		if err := runEntityList("TODO", "", "default", srv.URL, 20); err != nil {
			t.Fatalf("runEntityList: %v", err)
		}
	})

	t.Run("empty list prints message", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "success",
				"data":       []any{},
				"pagination": map[string]any{"page": 1, "size": 20, "total": 0},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		if err := runEntityList("TODO", "", "default", srv.URL, 20); err != nil {
			t.Fatalf("runEntityList empty: %v", err)
		}
	})

	t.Run("shows specific entity with fields", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Make-Target") != "MakeService.GetResource" {
				t.Errorf("unexpected X-Make-Target: %s", r.Header.Get("X-Make-Target"))
			}
			json.NewEncoder(w).Encode(map[string]any{
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

		if err := runEntityList("TODO", "项目", "default", srv.URL, 20); err != nil {
			t.Fatalf("runEntityList with name: %v", err)
		}
	})

	t.Run("shows specific entity with no fields", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{
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

		if err := runEntityList("TODO", "空实体", "default", srv.URL, 20); err != nil {
			t.Fatalf("runEntityList no fields: %v", err)
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		if err := runEntityList("TODO", "", "default", "http://localhost", 20); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		if err := runEntityList("TODO", "", "nonexistent", "http://localhost", 20); err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})

	t.Run("fails on list API error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"code": 500, "msg": "server error"})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		if err := runEntityList("TODO", "", "default", srv.URL, 20); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails on get API error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"code": 404, "msg": "entity not found"})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		if err := runEntityList("TODO", "不存在", "default", srv.URL, 20); err == nil {
			t.Fatal("expected error on get API failure")
		}
	})
}
