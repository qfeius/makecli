/**
 * [INPUT]: 依赖 cmd 包内的 runEntityCreate / loadFields（包内白盒），internal/config、encoding/json、net/http、net/http/httptest、os
 * [OUTPUT]: 覆盖 entity create 子命令核心逻辑的单元测试
 * [POS]: cmd 模块 entity_create.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRunEntityCreate(t *testing.T) {
	t.Run("creates entity with no fields", func(t *testing.T) {
		srv := newMockMeta(t, 200, "create entity success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		if err := runEntityCreate("项目", "TODO", "", "default", srv.URL); err != nil {
			t.Fatalf("runEntityCreate: %v", err)
		}
	})

	t.Run("creates entity with fields from file", func(t *testing.T) {
		srv := newMockMeta(t, 200, "create entity success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		fieldsFile := writeFieldsFile(t, []map[string]any{
			{"name": "项目名称", "type": "Make.Field.Text", "meta": map[string]any{"version": "1.0.0"}, "properties": nil},
		})

		if err := runEntityCreate("项目", "TODO", fieldsFile, "default", srv.URL); err != nil {
			t.Fatalf("runEntityCreate with fields: %v", err)
		}
	})

	t.Run("rejects field name starting with underscore", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		fieldsFile := writeFieldsFile(t, []map[string]any{
			{"name": "_内部字段", "type": "Make.Field.Text", "meta": map[string]any{"version": "1.0.0"}, "properties": nil},
		})

		if err := runEntityCreate("项目", "TODO", fieldsFile, "default", "http://localhost"); err == nil {
			t.Fatal("expected error for field name starting with _")
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())

		if err := runEntityCreate("项目", "TODO", "", "default", "http://localhost"); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails on API error response", func(t *testing.T) {
		srv := newMockMeta(t, 400, "invalid entity")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		if err := runEntityCreate("项目", "TODO", "", "default", srv.URL); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		if err := runEntityCreate("项目", "TODO", "", "nonexistent", "http://localhost"); err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})

	t.Run("fails with invalid fields file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		bad := filepath.Join(t.TempDir(), "bad.json")
		os.WriteFile(bad, []byte("not json"), 0644)

		if err := runEntityCreate("项目", "TODO", bad, "default", "http://localhost"); err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}

// writeFieldsFile 将 fields 写入临时 JSON 文件，返回路径
func writeFieldsFile(t *testing.T, fields []map[string]any) string {
	t.Helper()
	data, _ := json.Marshal(fields)
	path := filepath.Join(t.TempDir(), "fields.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}
