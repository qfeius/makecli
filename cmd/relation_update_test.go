/**
 * [INPUT]: 依赖 cmd 包内的 runRelationUpdate（包内白盒），internal/config、os、testing
 * [OUTPUT]: 覆盖 relation update 子命令核心逻辑的单元测试
 * [POS]: cmd 模块 relation_update.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunRelationUpdate(t *testing.T) {
	t.Run("updates relation successfully", func(t *testing.T) {
		srv := newMockMeta(t, 200, "update relation success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		jsonFile := writeRelationJSON(t, map[string]any{
			"from": map[string]any{"entityKey": "project", "cardinality": "one"},
			"to":   map[string]any{"entityKey": "task", "cardinality": "many"},
		})

		if err := runRelationUpdate("project_has_tasks", "项目任务关联", "TODO", jsonFile); err != nil {
			t.Fatalf("runRelationUpdate: %v", err)
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		MetaServerURL = "http://unused"

		jsonFile := writeRelationJSON(t, map[string]any{
			"from": map[string]any{"entityKey": "project", "cardinality": "one"},
			"to":   map[string]any{"entityKey": "task", "cardinality": "many"},
		})

		if err := runRelationUpdate("project_has_tasks", "项目任务关联", "TODO", jsonFile); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails on API error response", func(t *testing.T) {
		srv := newMockMeta(t, 400, "relation not found")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		jsonFile := writeRelationJSON(t, map[string]any{
			"from": map[string]any{"entityKey": "project", "cardinality": "one"},
			"to":   map[string]any{"entityKey": "task", "cardinality": "many"},
		})

		if err := runRelationUpdate("project_has_tasks", "项目任务关联", "TODO", jsonFile); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = "http://unused"
		setProfile(t, "nonexistent")

		jsonFile := writeRelationJSON(t, map[string]any{
			"from": map[string]any{"entityKey": "project", "cardinality": "one"},
			"to":   map[string]any{"entityKey": "task", "cardinality": "many"},
		})

		if err := runRelationUpdate("project_has_tasks", "项目任务关联", "TODO", jsonFile); err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})

	t.Run("fails with invalid JSON file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = "http://unused"

		bad := filepath.Join(t.TempDir(), "bad.json")
		_ = os.WriteFile(bad, []byte("not json"), 0644)

		if err := runRelationUpdate("project_has_tasks", "项目任务关联", "TODO", bad); err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}
