/**
 * [INPUT]: 依赖 cmd 包内的 runRelationCreate / loadRelationProperties（包内白盒），internal/config、encoding/json、os、testing
 * [OUTPUT]: 覆盖 relation create 子命令核心逻辑的单元测试
 * [POS]: cmd 模块 relation_create.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRunRelationCreate(t *testing.T) {
	t.Run("creates relation successfully", func(t *testing.T) {
		srv := newMockMeta(t, 200, "create relation success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		jsonFile := writeRelationJSON(t, map[string]any{
			"from": map[string]any{"entityKey": "project", "cardinality": "many"},
			"to":   map[string]any{"entityKey": "task", "cardinality": "one"},
		})

		if err := runRelationCreate("project_has_tasks", "项目任务关联", "TODO", jsonFile); err != nil {
			t.Fatalf("runRelationCreate: %v", err)
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		MetaServerURL = "http://unused"

		jsonFile := writeRelationJSON(t, map[string]any{
			"from": map[string]any{"entityKey": "project", "cardinality": "many"},
			"to":   map[string]any{"entityKey": "task", "cardinality": "one"},
		})

		if err := runRelationCreate("project_has_tasks", "项目任务关联", "TODO", jsonFile); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails on API error response", func(t *testing.T) {
		srv := newMockMeta(t, 400, "invalid relation")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		jsonFile := writeRelationJSON(t, map[string]any{
			"from": map[string]any{"entityKey": "project", "cardinality": "many"},
			"to":   map[string]any{"entityKey": "task", "cardinality": "one"},
		})

		if err := runRelationCreate("project_has_tasks", "项目任务关联", "TODO", jsonFile); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = "http://unused"
		setProfile(t, "nonexistent")

		jsonFile := writeRelationJSON(t, map[string]any{
			"from": map[string]any{"entityKey": "project", "cardinality": "many"},
			"to":   map[string]any{"entityKey": "task", "cardinality": "one"},
		})

		if err := runRelationCreate("project_has_tasks", "项目任务关联", "TODO", jsonFile); err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})

	t.Run("fails with invalid JSON file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = "http://unused"

		bad := filepath.Join(t.TempDir(), "bad.json")
		_ = os.WriteFile(bad, []byte("not json"), 0644)

		if err := runRelationCreate("project_has_tasks", "项目任务关联", "TODO", bad); err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("fails with nonexistent JSON file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = "http://unused"

		if err := runRelationCreate("project_has_tasks", "项目任务关联", "TODO", "/nonexistent.json"); err == nil {
			t.Fatal("expected error for nonexistent file")
		}
	})
}

// writeRelationJSON 将 relation properties 写入临时 JSON 文件，返回路径
func writeRelationJSON(t *testing.T, props map[string]any) string {
	t.Helper()
	data, _ := json.Marshal(props)
	path := filepath.Join(t.TempDir(), "relation.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}
