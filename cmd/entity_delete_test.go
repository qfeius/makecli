/**
 * [INPUT]: 依赖 cmd 包内的 runEntityDelete（包内白盒），internal/config、net/http/httptest、testing
 * [OUTPUT]: 覆盖 entity delete 子命令核心逻辑的单元测试
 * [POS]: cmd 模块 entity_delete.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"testing"
)

func TestRunEntityDelete(t *testing.T) {
	t.Run("deletes entity successfully", func(t *testing.T) {
		srv := newMockMeta(t, 200, "delete entity success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		if err := runEntityDelete("Project", "TODO", "default", srv.URL); err != nil {
			t.Fatalf("runEntityDelete: %v", err)
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())

		if err := runEntityDelete("Project", "TODO", "default", "http://localhost"); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails on API error response", func(t *testing.T) {
		srv := newMockMeta(t, 400, "invalid entity")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		if err := runEntityDelete("Project", "TODO", "default", srv.URL); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		if err := runEntityDelete("Project", "TODO", "nonexistent", "http://localhost"); err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})
}
