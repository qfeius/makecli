/**
 * [INPUT]: 依赖 cmd 包内的 runRelationList（包内白盒），internal/config、encoding/json、net/http、net/http/httptest
 * [OUTPUT]: 覆盖 relation list 子命令核心逻辑的单元测试（列表/空列表/具体relation/JSON输出/无凭证/API错误/未知profile）
 * [POS]: cmd 模块 relation_list.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
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

func TestRunRelationList(t *testing.T) {
	t.Run("lists relations successfully", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Make-Target") != "MakeService.ListResources" {
				t.Errorf("unexpected X-Make-Target: %s", r.Header.Get("X-Make-Target"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "success",
				"data": []map[string]any{
					{
						"name": "project-has-tasks", "type": "Make.Relation", "app": "TODO",
						"meta": map[string]any{"version": "1.0.0"},
						"properties": map[string]any{
							"from": map[string]any{"entity": "项目", "cardinality": "many"},
							"to":   map[string]any{"entity": "任务", "cardinality": "one"},
						},
					},
				},
				"pagination": map[string]any{"page": 1, "size": 20, "total": 1},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		if err := runRelationList("TODO", "", "default", 1, 20, outputTable); err != nil {
			t.Fatalf("runRelationList: %v", err)
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
		ServerURL = srv.URL

		if err := runRelationList("TODO", "", "default", 1, 20, outputTable); err != nil {
			t.Fatalf("runRelationList empty: %v", err)
		}
	})

	t.Run("prints list as json when requested", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "success",
				"data": []map[string]any{
					{
						"name": "project-has-tasks", "type": "Make.Relation", "app": "TODO",
						"meta": map[string]any{"version": "1.0.0"},
						"properties": map[string]any{
							"from": map[string]any{"entity": "项目", "cardinality": "many"},
							"to":   map[string]any{"entity": "任务", "cardinality": "one"},
						},
					},
				},
				"pagination": map[string]any{"page": 1, "size": 20, "total": 1},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		output := captureStdout(t, func() {
			if err := runRelationList("TODO", "", "default", 1, 20, outputJSON); err != nil {
				t.Fatalf("runRelationList json: %v", err)
			}
		})

		if !strings.Contains(output, "\"data\"") {
			t.Fatalf("expected JSON output, got %q", output)
		}
		if !strings.Contains(output, "\"count\": 1") {
			t.Fatalf("expected pagination count in JSON output, got %q", output)
		}
	})

	t.Run("shows specific relation", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Make-Target") != "MakeService.GetResource" {
				t.Errorf("unexpected X-Make-Target: %s", r.Header.Get("X-Make-Target"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "success",
				"data": map[string]any{
					"name": "project-has-tasks", "type": "Make.Relation", "app": "TODO",
					"meta": map[string]any{"version": "1.0.0"},
					"properties": map[string]any{
						"from": map[string]any{"entity": "项目", "cardinality": "many"},
						"to":   map[string]any{"entity": "任务", "cardinality": "one"},
					},
				},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		out := captureStdout(t, func() {
			if err := runRelationList("TODO", "project-has-tasks", "default", 1, 20, outputTable); err != nil {
				t.Fatalf("runRelationList detail: %v", err)
			}
		})

		if !strings.Contains(out, "project-has-tasks") {
			t.Fatalf("expected relation name in output, got %q", out)
		}
		if !strings.Contains(out, "项目") {
			t.Fatalf("expected from entity in output, got %q", out)
		}
		if !strings.Contains(out, "任务") {
			t.Fatalf("expected to entity in output, got %q", out)
		}
	})

	t.Run("prints specific relation as json when requested", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "success",
				"data": map[string]any{
					"name": "project-has-tasks", "type": "Make.Relation", "app": "TODO",
					"meta": map[string]any{"version": "1.0.0"},
					"properties": map[string]any{
						"from": map[string]any{"entity": "项目", "cardinality": "many"},
						"to":   map[string]any{"entity": "任务", "cardinality": "one"},
					},
				},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		output := captureStdout(t, func() {
			if err := runRelationList("TODO", "project-has-tasks", "default", 1, 20, outputJSON); err != nil {
				t.Fatalf("runRelationList json detail: %v", err)
			}
		})

		if !strings.Contains(output, "\"name\": \"project-has-tasks\"") {
			t.Fatalf("expected relation name in JSON output, got %q", output)
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ServerURL = "http://unused"
		if err := runRelationList("TODO", "", "default", 1, 20, outputTable); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = "http://unused"
		if err := runRelationList("TODO", "", "nonexistent", 1, 20, outputTable); err == nil {
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
		ServerURL = srv.URL

		if err := runRelationList("TODO", "", "default", 1, 20, outputTable); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails on get API error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 404, "msg": "relation not found"})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		if err := runRelationList("TODO", "不存在", "default", 1, 20, outputTable); err == nil {
			t.Fatal("expected error on get API failure")
		}
	})

	t.Run("fails when page is less than 1", func(t *testing.T) {
		if err := runRelationList("TODO", "", "default", 0, 20, outputTable); err == nil {
			t.Fatal("expected error for invalid page")
		}
	})

	t.Run("fails when size is less than 1", func(t *testing.T) {
		if err := runRelationList("TODO", "", "default", 1, 0, outputTable); err == nil {
			t.Fatal("expected error for invalid size")
		}
	})

	t.Run("fails on unsupported output format", func(t *testing.T) {
		if err := runRelationList("TODO", "", "default", 1, 20, "xml"); err == nil {
			t.Fatal("expected error for unsupported output format")
		}
	})
}
