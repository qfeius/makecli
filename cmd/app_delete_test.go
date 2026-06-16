/**
 * [INPUT]: 依赖 cmd 包内的 runAppDelete/runAppDeleteFromFile/confirmDeleteFunc（包内白盒），internal/config、errors、path/filepath
 * [OUTPUT]: 覆盖 app delete 子命令核心逻辑的单元测试（含 -f 文件模式与删除确认门控）
 * [POS]: cmd 模块 app_delete.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证、打桩 confirmDeleteFunc 隔离终端交互
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"errors"
	"path/filepath"
	"testing"
)

// stubConfirm 临时替换 confirmDeleteFunc，t.Cleanup 自动还原，隔离真实终端交互
func stubConfirm(t *testing.T, err error) {
	t.Helper()
	orig := confirmDeleteFunc
	confirmDeleteFunc = func(string) error { return err }
	t.Cleanup(func() { confirmDeleteFunc = orig })
}

func TestRunAppDelete(t *testing.T) {
	t.Run("deletes app via API with --yes", func(t *testing.T) {
		srv := newMockMeta(t, 200, "delete app success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		if err := runAppDelete("myapp", true); err != nil {
			t.Fatalf("runAppDelete: %v", err)
		}
	})

	t.Run("deletes app after confirmation succeeds", func(t *testing.T) {
		stubConfirm(t, nil)
		srv := newMockMeta(t, 200, "delete app success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		if err := runAppDelete("myapp", false); err != nil {
			t.Fatalf("runAppDelete: %v", err)
		}
	})

	t.Run("confirmation refusal stops before API", func(t *testing.T) {
		sentinel := errors.New("declined")
		stubConfirm(t, sentinel)
		// 没有 mock server 也没有凭证：确认失败必须在触网/读凭证前短路
		t.Setenv("HOME", t.TempDir())
		MetaServerURL = "http://unused"

		if err := runAppDelete("myapp", false); !errors.Is(err, sentinel) {
			t.Fatalf("expected confirmation error, got %v", err)
		}
	})

	t.Run("real confirm gate refuses in non-interactive shell", func(t *testing.T) {
		// 不打桩，走真 confirmDeleteByTypingKey；go test 下 stdin 非 TTY，应直接拒绝
		t.Setenv("HOME", t.TempDir())
		MetaServerURL = "http://unused"
		if err := runAppDelete("myapp", false); err == nil {
			t.Fatal("expected refusal without --yes in non-interactive shell")
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		MetaServerURL = "http://unused"
		if err := runAppDelete("myapp", true); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails on API error response", func(t *testing.T) {
		srv := newMockMeta(t, 400, "app not found")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		if err := runAppDelete("myapp", true); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = "http://unused"
		setProfile(t, "nonexistent")

		if err := runAppDelete("myapp", true); err == nil {
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
		MetaServerURL = srv.URL

		f := filepath.Join(t.TempDir(), "app.yaml")
		writeTestFile(t, f, []byte("key: fileapp\nname: 文件应用\ntype: Make.App\n"))

		if err := runAppDeleteFromFile(f, true); err != nil {
			t.Fatalf("runAppDeleteFromFile: %v", err)
		}
	})

	t.Run("fails on non-yaml file", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "app.txt")
		writeTestFile(t, f, []byte("name: foo"))

		if err := runAppDeleteFromFile(f, true); err == nil {
			t.Fatal("expected error for non-yaml file")
		}
	})

	t.Run("fails when no Make.App in file", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "entity.yaml")
		writeTestFile(t, f, []byte("key: foo\nname: 实体\ntype: Make.Entity\nappKey: bar\n"))

		if err := runAppDeleteFromFile(f, true); err == nil {
			t.Fatal("expected error for missing Make.App")
		}
	})
}
