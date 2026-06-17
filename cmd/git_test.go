/**
 * [INPUT]: 依赖 cmd 包内的 openRepo/initGitRepo/ensureGitignore/stageAndCommit/assertDeployable/gitSignature（包内白盒）、chdir/writeTestFile 辅助，os、path/filepath、strings、testing、github.com/go-git/go-git/v5
 * [OUTPUT]: 覆盖共享 go-git 原语层的单元测试（建仓幂等 / .gitignore 增量补齐 / 暂存提交 / 部署门控 / 署名 LocalScope 回归守护）
 * [POS]: cmd 模块 git.go 的配套测试，用 t.TempDir 隔离文件系统、真实 go-git 验证行为
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
)

func TestInitGitRepo(t *testing.T) {
	t.Run("inits an empty dir then is idempotent", func(t *testing.T) {
		dir := t.TempDir()
		created, err := initGitRepo(dir)
		if err != nil || !created {
			t.Fatalf("first init: created=%v err=%v, want created=true", created, err)
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
			t.Errorf(".git should exist after init: %v", err)
		}
		created, err = initGitRepo(dir)
		if err != nil || created {
			t.Errorf("re-init: created=%v err=%v, want created=false", created, err)
		}
	})
}

func TestEnsureGitignore(t *testing.T) {
	t.Run("writes full template when absent", func(t *testing.T) {
		dir := t.TempDir()
		changed, err := ensureGitignore(dir)
		if err != nil || !changed {
			t.Fatalf("ensureGitignore: changed=%v err=%v, want changed=true", changed, err)
		}
		data, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"node_modules/", ".env", ".DS_Store"} {
			if !strings.Contains(string(data), want) {
				t.Errorf(".gitignore missing %q:\n%s", want, data)
			}
		}
	})

	t.Run("appends only missing entries, preserves custom lines", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, ".gitignore")
		writeTestFile(t, path, []byte("node_modules/\nmy-custom-secret.key\n"))

		changed, err := ensureGitignore(dir)
		if err != nil || !changed {
			t.Fatalf("ensureGitignore: changed=%v err=%v, want changed=true", changed, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		got := string(data)
		if !strings.Contains(got, "my-custom-secret.key") {
			t.Errorf("custom line must be preserved:\n%s", got)
		}
		if !strings.Contains(got, ".DS_Store") {
			t.Errorf("missing entry .DS_Store should be appended:\n%s", got)
		}
		// node_modules/ 已存在，不应重复追加
		if strings.Count(got, "node_modules/") != 1 {
			t.Errorf("node_modules/ should appear exactly once:\n%s", got)
		}
	})

	t.Run("no change when already complete", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := ensureGitignore(dir); err != nil { // 先写全
			t.Fatal(err)
		}
		before, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
		changed, err := ensureGitignore(dir)
		if err != nil || changed {
			t.Errorf("second ensure: changed=%v err=%v, want changed=false", changed, err)
		}
		after, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
		if !bytes.Equal(before, after) {
			t.Errorf("complete .gitignore must not be rewritten")
		}
	})
}

func TestStageAndCommit(t *testing.T) {
	t.Run("commits changes then clean is a no-op", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, "a.txt"), []byte("hi"))
		if _, err := initGitRepo(dir); err != nil {
			t.Fatal(err)
		}
		repo, err := git.PlainOpen(dir)
		if err != nil {
			t.Fatal(err)
		}

		committed, err := stageAndCommit(repo, "first")
		if err != nil || !committed {
			t.Fatalf("first commit: committed=%v err=%v, want true", committed, err)
		}
		if _, err := repo.Head(); err != nil {
			t.Errorf("HEAD should exist after commit: %v", err)
		}

		committed, err = stageAndCommit(repo, "second")
		if err != nil || committed {
			t.Errorf("clean commit: committed=%v err=%v, want false", committed, err)
		}
	})

	t.Run("respects .gitignore", func(t *testing.T) {
		dir := t.TempDir()
		writeTestFile(t, filepath.Join(dir, ".gitignore"), []byte("secret.txt\n"))
		writeTestFile(t, filepath.Join(dir, "keep.txt"), []byte("keep"))
		writeTestFile(t, filepath.Join(dir, "secret.txt"), []byte("token"))
		if _, err := initGitRepo(dir); err != nil {
			t.Fatal(err)
		}
		repo, err := git.PlainOpen(dir)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := stageAndCommit(repo, "init"); err != nil {
			t.Fatal(err)
		}

		head, _ := repo.Head()
		c, _ := repo.CommitObject(head.Hash())
		tree, _ := c.Tree()
		if _, err := tree.File("keep.txt"); err != nil {
			t.Errorf("keep.txt should be committed: %v", err)
		}
		if _, err := tree.File("secret.txt"); err == nil {
			t.Error("secret.txt should be gitignored, not committed")
		}
	})
}

func TestAssertDeployable(t *testing.T) {
	newRepo := func(t *testing.T) (*git.Repository, string) {
		t.Helper()
		dir := t.TempDir()
		if _, err := initGitRepo(dir); err != nil {
			t.Fatal(err)
		}
		repo, err := git.PlainOpen(dir)
		if err != nil {
			t.Fatal(err)
		}
		return repo, dir
	}

	t.Run("rejects a repo with no commits", func(t *testing.T) {
		repo, _ := newRepo(t)
		if err := assertDeployable(repo); err == nil {
			t.Error("expected error for repo with no HEAD")
		}
	})

	t.Run("accepts a clean committed repo", func(t *testing.T) {
		repo, dir := newRepo(t)
		writeTestFile(t, filepath.Join(dir, "a.txt"), []byte("x"))
		if _, err := stageAndCommit(repo, "c"); err != nil {
			t.Fatal(err)
		}
		if err := assertDeployable(repo); err != nil {
			t.Errorf("clean committed repo should be deployable: %v", err)
		}
	})

	t.Run("rejects a dirty working tree", func(t *testing.T) {
		repo, dir := newRepo(t)
		writeTestFile(t, filepath.Join(dir, "a.txt"), []byte("x"))
		if _, err := stageAndCommit(repo, "c"); err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, filepath.Join(dir, "b.txt"), []byte("untracked"))
		err := assertDeployable(repo)
		if err == nil {
			t.Fatal("expected error for dirty tree")
		}
		if !strings.Contains(err.Error(), "uncommitted") {
			t.Errorf("error should mention uncommitted, got: %v", err)
		}
	})
}

func TestOpenRepo(t *testing.T) {
	t.Run("errors with init guidance when absent", func(t *testing.T) {
		chdir(t, t.TempDir())
		_, err := openRepo()
		if err == nil {
			t.Fatal("expected error when no repository")
		}
		if !strings.Contains(err.Error(), "app init") {
			t.Errorf("error should guide to `app init`, got: %v", err)
		}
	})

	t.Run("opens an existing repo", func(t *testing.T) {
		dir := t.TempDir()
		chdir(t, dir)
		if _, err := initGitRepo(dir); err != nil {
			t.Fatal(err)
		}
		if _, err := openRepo(); err != nil {
			t.Errorf("openRepo should find the repo: %v", err)
		}
	})
}

func TestGitSignature(t *testing.T) {
	t.Run("falls back to makecli identity when unconfigured", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir()) // 无全局 gitconfig
		dir := t.TempDir()
		if _, err := initGitRepo(dir); err != nil {
			t.Fatal(err)
		}
		repo, err := git.PlainOpen(dir)
		if err != nil {
			t.Fatal(err)
		}
		sig := gitSignature(repo)
		if sig.Name != "makecli" {
			t.Errorf("unconfigured signature name = %q, want makecli", sig.Name)
		}
	})

	t.Run("reads user identity from git config (LocalScope picks it up)", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		dir := t.TempDir()
		if _, err := initGitRepo(dir); err != nil {
			t.Fatal(err)
		}
		repo, err := git.PlainOpen(dir)
		if err != nil {
			t.Fatal(err)
		}
		cfg, err := repo.Config()
		if err != nil {
			t.Fatal(err)
		}
		cfg.User.Name = "Jim Yu"
		cfg.User.Email = "jim@example.com"
		if err := repo.SetConfig(cfg); err != nil {
			t.Fatal(err)
		}

		sig := gitSignature(repo)
		if sig.Name != "Jim Yu" || sig.Email != "jim@example.com" {
			t.Errorf("signature = %s <%s>, want Jim Yu <jim@example.com> (SystemScope bug would miss local config)", sig.Name, sig.Email)
		}
	})
}
