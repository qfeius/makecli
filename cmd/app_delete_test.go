/**
 * [INPUT]: 依赖 cmd 包内的 runAppDelete/runAppDeleteFromFile（包内白盒），internal/config、os、path/filepath
 * [OUTPUT]: 覆盖 app delete 子命令核心逻辑的单元测试（含 -f 文件模式）
 * [POS]: cmd 模块 app_delete.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"path/filepath"
	"testing"
)

func TestRunAppDelete(t *testing.T) {
	t.Run("deletes app via API", func(t *testing.T) {
		srv := newMockMeta(t, 200, "delete app success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		if err := runAppDelete("myapp", "default"); err != nil {
			t.Fatalf("runAppDelete: %v", err)
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ServerURL = "http://unused"
		if err := runAppDelete("myapp", "default"); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails on API error response", func(t *testing.T) {
		srv := newMockMeta(t, 400, "app not found")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		if err := runAppDelete("myapp", "default"); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = "http://unused"

		if err := runAppDelete("myapp", "nonexistent"); err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})
}

func TestRunAppDeleteFromFile(t *testing.T) {
	t.Run("deletes app from YAML file", func(t *testing.T) {
		srv := newMockMeta(t, 200, "delete app success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		f := filepath.Join(t.TempDir(), "app.yaml")
		writeTestFile(t, f, []byte("name: fileapp\ntype: Make.App\n"))

		if err := runAppDeleteFromFile(f, "default"); err != nil {
			t.Fatalf("runAppDeleteFromFile: %v", err)
		}
	})

	t.Run("fails on non-yaml file", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "app.txt")
		writeTestFile(t, f, []byte("name: foo"))

		if err := runAppDeleteFromFile(f, "default"); err == nil {
			t.Fatal("expected error for non-yaml file")
		}
	})

	t.Run("fails when no Make.App in file", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "entity.yaml")
		writeTestFile(t, f, []byte("name: foo\ntype: Make.Entity\napp: bar\n"))

		if err := runAppDeleteFromFile(f, "default"); err == nil {
			t.Fatal("expected error for missing Make.App")
		}
	})
}
