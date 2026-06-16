/**
 * [INPUT]: 依赖 cmd 包内的 runRelationDelete（包内白盒），internal/config、testing
 * [OUTPUT]: 覆盖 relation delete 子命令核心逻辑的单元测试
 * [POS]: cmd 模块 relation_delete.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"testing"
)

func TestRunRelationDelete(t *testing.T) {
	t.Run("deletes relation successfully", func(t *testing.T) {
		srv := newMockMeta(t, 200, "delete relation success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		if err := runRelationDelete("project-has-tasks", "TODO"); err != nil {
			t.Fatalf("runRelationDelete: %v", err)
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		MetaServerURL = "http://unused"

		if err := runRelationDelete("project-has-tasks", "TODO"); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails on API error response", func(t *testing.T) {
		srv := newMockMeta(t, 400, "relation not found")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		if err := runRelationDelete("project-has-tasks", "TODO"); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = "http://unused"
		setProfile(t, "nonexistent")

		if err := runRelationDelete("project-has-tasks", "TODO"); err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})
}
