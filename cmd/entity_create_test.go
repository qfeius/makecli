/**
 * [INPUT]: 依赖 cmd 包内的 runEntityCreate / loadFields（包内白盒），internal/config、encoding/json、net/http、net/http/httptest、os、strings
 * [OUTPUT]: 覆盖 entity create 子命令核心逻辑的单元测试（含 --dry-run：X-Dry-Run 头到达线缆 + would-be 输出）
 * [POS]: cmd 模块 entity_create.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunEntityCreate(t *testing.T) {
	t.Run("creates entity with no fields", func(t *testing.T) {
		srv := newMockMeta(t, 200, "create entity success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		if err := runEntityCreate("project", "项目", "TODO", "", false); err != nil {
			t.Fatalf("runEntityCreate: %v", err)
		}
	})

	t.Run("creates entity with fields from file", func(t *testing.T) {
		srv := newMockMeta(t, 200, "create entity success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		fieldsFile := writeFieldsFile(t, []map[string]any{
			{"key": "project_name", "name": "项目名称", "type": "Make.Field.Text", "meta": map[string]any{"version": "1.0.0"}, "properties": nil},
		})

		if err := runEntityCreate("project", "项目", "TODO", fieldsFile, false); err != nil {
			t.Fatalf("runEntityCreate with fields: %v", err)
		}
	})

	t.Run("rejects field key starting with underscore", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = "http://unused"

		fieldsFile := writeFieldsFile(t, []map[string]any{
			{"key": "_internal_field", "name": "内部字段", "type": "Make.Field.Text", "meta": map[string]any{"version": "1.0.0"}, "properties": nil},
		})

		if err := runEntityCreate("project", "项目", "TODO", fieldsFile, false); err == nil {
			t.Fatal("expected error for field key starting with _")
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		MetaServerURL = "http://unused"

		if err := runEntityCreate("project", "项目", "TODO", "", false); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails on API error response", func(t *testing.T) {
		srv := newMockMeta(t, 400, "invalid entity")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		if err := runEntityCreate("project", "项目", "TODO", "", false); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = "http://unused"
		setProfile(t, "nonexistent")

		if err := runEntityCreate("project", "项目", "TODO", "", false); err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})

	t.Run("dry-run sends X-Dry-Run and prints would-be line", func(t *testing.T) {
		var gotDryRun string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotDryRun = r.Header.Get("X-Dry-Run")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "create entity success"})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		out := captureStdout(t, func() {
			if err := runEntityCreate("project", "项目", "TODO", "", true); err != nil {
				t.Fatalf("runEntityCreate dry-run: %v", err)
			}
		})
		if gotDryRun != "true" {
			t.Errorf("X-Dry-Run header = %q, want %q", gotDryRun, "true")
		}
		if !strings.Contains(out, "Dry run") || !strings.Contains(out, "would be created") {
			t.Errorf("dry-run output = %q, want a would-be 'Dry run' line", out)
		}
	})

	t.Run("fails with invalid fields file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = "http://unused"

		bad := filepath.Join(t.TempDir(), "bad.json")
		_ = os.WriteFile(bad, []byte("not json"), 0644)

		if err := runEntityCreate("project", "项目", "TODO", bad, false); err == nil {
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
