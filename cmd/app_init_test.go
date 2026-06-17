/**
 * [INPUT]: 依赖 cmd 包内的 runAppInit（包内白盒）、captureStdout/writeTestFile 辅助，os、path/filepath、strings、testing
 * [OUTPUT]: 覆盖 app init 子命令的单元测试（建仓 + .gitignore 补齐 + 幂等 + 状态输出）
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
	t.Run("inits git and writes .gitignore", func(t *testing.T) {
		dir := t.TempDir()
		out := captureStdout(t, func() {
			if err := runAppInit(dir); err != nil {
				t.Fatalf("runAppInit: %v", err)
			}
		})

		if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
			t.Errorf(".git should exist: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, ".gitignore")); err != nil {
			t.Errorf(".gitignore should exist: %v", err)
		}
		if !strings.Contains(out, "initialized") {
			t.Errorf("output should report git initialized, got: %q", out)
		}
		if !strings.Contains(out, "updated") {
			t.Errorf("output should report .gitignore updated, got: %q", out)
		}
	})

	t.Run("is idempotent on re-run", func(t *testing.T) {
		dir := t.TempDir()
		if err := runAppInit(dir); err != nil {
			t.Fatalf("first init: %v", err)
		}
		out := captureStdout(t, func() {
			if err := runAppInit(dir); err != nil {
				t.Fatalf("second init: %v", err)
			}
		})
		if !strings.Contains(out, "already a repository") {
			t.Errorf("re-run should report already a repository, got: %q", out)
		}
		if !strings.Contains(out, "already complete") {
			t.Errorf("re-run should report .gitignore already complete, got: %q", out)
		}
	})

	t.Run("does not commit", func(t *testing.T) {
		// app init 刻意不 commit——仓库应有 .git 但无 HEAD（提交时机交还用户）
		dir := t.TempDir()
		if err := runAppInit(dir); err != nil {
			t.Fatalf("runAppInit: %v", err)
		}
		if _, err := initGitRepo(dir); err != nil { // 幂等，仍是同一仓库
			t.Fatal(err)
		}
		// 通过 assertDeployable 间接验证「无提交」——应因无 HEAD 报错
		// （openRepo 走 cwd，这里直接用低层判断）
		if _, err := os.Stat(filepath.Join(dir, ".git", "refs", "heads")); err != nil {
			t.Fatalf("refs/heads should exist: %v", err)
		}
		entries, err := os.ReadDir(filepath.Join(dir, ".git", "refs", "heads"))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Errorf("app init must not create any branch/commit, found refs: %v", entries)
		}
	})
}
