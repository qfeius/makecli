/**
 * [INPUT]: 依赖 cmd 包内的 runAppInit（包内白盒）、captureStdout/writeTestFile/chdir 辅助、loadAppManifestFromFile，os、path/filepath、strings、testing
 * [OUTPUT]: 覆盖 app init 子命令的单元测试（完整本地脚手架 + 生成 AGENTS.md 导航契约 + git + .gitignore + 幂等 + 不 commit + appKey 推导/默认 cwd）
 * [POS]: cmd 模块 app_init.go 的配套测试，用 t.TempDir 隔离文件系统、captureStdout 验证状态行
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAppInit(t *testing.T) {
	t.Run("scaffolds a full local project: files + git + .gitignore", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "shop")
		out := captureStdout(t, func() {
			if err := runAppInit(dir); err != nil {
				t.Fatalf("runAppInit: %v", err)
			}
		})

		for _, name := range []string{"CLAUDE.md", "AGENTS.md", filepath.Join("apps", "dsl", "app.yaml"), ".gitignore", ".git"} {
			if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
				t.Errorf("%s should exist after init: %v", name, err)
			}
		}
		agents, err := os.ReadFile(filepath.Join(dir, "AGENTS.md"))
		if err != nil {
			t.Fatalf("read AGENTS.md: %v", err)
		}
		assertGeneratedAgentsContract(t, string(agents))
		// app.yaml 的 key 取自目录名
		m, err := loadAppManifestFromFile(filepath.Join(dir, "apps", "dsl", "app.yaml"))
		if err != nil {
			t.Fatalf("loadAppManifestFromFile: %v", err)
		}
		if m.Key != "shop" {
			t.Errorf("app.yaml key = %q, want shop (from dir name)", m.Key)
		}
		for _, want := range []string{"created", "initialized", "updated"} {
			if !strings.Contains(out, want) {
				t.Errorf("output should report %q, got:\n%s", want, out)
			}
		}
	})

	t.Run("defaults appKey to cwd name when no arg", func(t *testing.T) {
		work := filepath.Join(t.TempDir(), "mycwdapp")
		if err := os.MkdirAll(work, 0755); err != nil {
			t.Fatal(err)
		}
		chdir(t, work)
		if err := runAppInit("."); err != nil {
			t.Fatalf("runAppInit '.': %v", err)
		}
		m, err := loadAppManifestFromFile(filepath.Join("apps", "dsl", "app.yaml"))
		if err != nil {
			t.Fatalf("loadAppManifestFromFile: %v", err)
		}
		if m.Key != "mycwdapp" {
			t.Errorf("app.yaml key = %q, want mycwdapp (from cwd)", m.Key)
		}
	})

	t.Run("rejects an invalid directory name as app key", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "bad-name")
		if err := runAppInit(dir); err == nil {
			t.Fatal("expected error for invalid key derived from dir name")
		}
	})

	t.Run("is idempotent: re-run reports already-present, preserves edits", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "shop")
		if err := runAppInit(dir); err != nil {
			t.Fatalf("first init: %v", err)
		}
		edited := filepath.Join(dir, "CLAUDE.md")
		writeTestFile(t, edited, []byte("MY EDITS"))

		out := captureStdout(t, func() {
			if err := runAppInit(dir); err != nil {
				t.Fatalf("second init: %v", err)
			}
		})
		for _, want := range []string{"exists", "already a repository", "already complete"} {
			if !strings.Contains(out, want) {
				t.Errorf("re-run should report %q, got:\n%s", want, out)
			}
		}
		if data, _ := os.ReadFile(edited); string(data) != "MY EDITS" {
			t.Errorf("init must not clobber user edits, got: %q", data)
		}
	})

	t.Run("does not commit (no HEAD)", func(t *testing.T) {
		// app init 刻意不 commit——仓库应有 .git 但无任何分支/提交（提交时机交还用户）
		dir := filepath.Join(t.TempDir(), "shop")
		if err := runAppInit(dir); err != nil {
			t.Fatalf("runAppInit: %v", err)
		}
		entries, err := os.ReadDir(filepath.Join(dir, ".git", "refs", "heads"))
		if err != nil {
			t.Fatalf("refs/heads should exist: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("app init must not create any branch/commit, found refs: %v", entries)
		}
	})
}
