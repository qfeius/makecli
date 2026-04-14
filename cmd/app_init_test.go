/**
 * [INPUT]: 依赖 cmd 包内的 runAppInit（包内白盒），os、path/filepath、github.com/qfeius/makecli/agents
 * [OUTPUT]: 覆盖 app init 子命令核心逻辑的单元测试
 * [POS]: cmd 模块 app_init.go 的配套测试
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qfeius/makecli/agents"
)

func TestRunAppInit(t *testing.T) {
	t.Run("creates both config files", func(t *testing.T) {
		dir := t.TempDir()
		if err := runAppInit(dir); err != nil {
			t.Fatalf("runAppInit: %v", err)
		}
		for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
			if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
				t.Errorf("expected %s to exist: %v", name, err)
			}
		}
	})

	t.Run("creates folder if not exists", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "newapp")
		if err := runAppInit(dir); err != nil {
			t.Fatalf("runAppInit: %v", err)
		}
		for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
			if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
				t.Errorf("expected %s to exist: %v", name, err)
			}
		}
	})

	t.Run("content matches embedded templates", func(t *testing.T) {
		dir := t.TempDir()
		if err := runAppInit(dir); err != nil {
			t.Fatalf("runAppInit: %v", err)
		}
		for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
			want, _ := agents.Templates.ReadFile(name)
			got, _ := os.ReadFile(filepath.Join(dir, name))
			if string(got) != string(want) {
				t.Errorf("%s content mismatch", name)
			}
		}
	})

	t.Run("fails if CLAUDE.md already exists", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("x"), 0644)
		if err := runAppInit(dir); err == nil {
			t.Fatal("expected error for existing CLAUDE.md")
		}
	})

	t.Run("fails if AGENTS.md already exists", func(t *testing.T) {
		dir := t.TempDir()
		_ = os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("x"), 0644)
		if err := runAppInit(dir); err == nil {
			t.Fatal("expected error for existing AGENTS.md")
		}
	})
}
