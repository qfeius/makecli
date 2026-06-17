/**
 * [INPUT]: 依赖 cmd 包内的 runDeploy / pushCurrentHead / gitPushFunc / initGitRepo / stageAndCommit（包内白盒）、enterAppDir(写 apps/dsl/app.yaml + chdir)、gitCommitAll(init+commit 当前目录)，encoding/json、errors、fmt、net/http、net/http/httptest、os、path/filepath、strings、testing、github.com/go-git/go-git/v5（及 plumbing/object 子包）
 * [OUTPUT]: 覆盖 deploy 子命令核心逻辑的单元测试（runDeploy 编排：本地真仓库门控 + gitPushFunc 桩隔离推送；pushCurrentHead 真 go-git 推到本地裸仓库；fail-fast 脏/无仓库/无提交报错且不触网）
 * [POS]: cmd 模块 deploy.go 的配套测试，用 httptest 隔离网络、gitPushFunc 打桩隔离推送、临时裸仓库做本地 remote 验证真实 go-git 行为
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// pushCall 打桩 gitPushFunc：记录 runDeploy 传入的推送参数，按 err 返回。
type pushCall struct {
	cloneURL string
	token    string
	force    bool
	called   bool
	err      error
}

func (p *pushCall) install(t *testing.T) {
	t.Helper()
	old := gitPushFunc
	gitPushFunc = func(_ *git.Repository, cloneURL, token string, force bool) error {
		p.called = true
		p.cloneURL, p.token, p.force = cloneURL, token, force
		return p.err
	}
	t.Cleanup(func() { gitPushFunc = old })
}

// enterAppDir 切到一个含 apps/dsl/app.yaml（key=<key>）的临时工程根目录。
// deploy 的 app 身份取自 DSL 文件而非目录名——临时目录名是随机的，证明 key 来自 app.yaml。
func enterAppDir(t *testing.T, key string) {
	t.Helper()
	dir := t.TempDir()
	chdir(t, dir)
	dslDir := filepath.Join(dir, "apps", "dsl")
	if err := os.MkdirAll(dslDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("key: %s\nname: %s\ntype: Make.App\nmeta:\n  version: 1.0.0\nproperties: {}\n", key, key)
	writeTestFile(t, filepath.Join(dslDir, "app.yaml"), []byte(content))
}

// gitCommitAll 在 cwd 就地 init 仓库并提交全部文件，留下一个有 HEAD、工作树干净的可部署仓库。
func gitCommitAll(t *testing.T) {
	t.Helper()
	if _, err := initGitRepo("."); err != nil {
		t.Fatal(err)
	}
	repo, err := git.PlainOpen(".")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stageAndCommit(repo, "test commit"); err != nil {
		t.Fatal(err)
	}
}

// newMockRepoServer 启动返回双环境仓库响应的代码仓库服务 mock
func newMockRepoServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200, "msg": "repositories are ready",
			"data": map[string]any{
				"appKey": "myapp", "type": "Make.Code.Repository",
				"properties": map[string]any{
					"env": map[string]any{
						"preview":    map[string]any{"repository": map[string]any{"cloneUrl": "https://repo.example/org/myapp-preview.git"}},
						"production": map[string]any{"repository": map[string]any{"cloneUrl": "https://repo.example/org/myapp-production.git"}},
					},
				},
			},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// noNetRepoServer 启动一个被调用即令测试失败的仓库服务——证明 fail-fast 在网络之前短路。
func noNetRepoServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Error("repository service must not be called when local git gate fails")
	}))
	t.Cleanup(srv.Close)
	return srv
}

// setupDeployEnv 准备工程目录(app.yaml key=myapp，已 init+commit 干净) + 凭证 + repo server 指向，返回安装好的 push 桩
func setupDeployEnv(t *testing.T) *pushCall {
	t.Helper()
	enterAppDir(t, "myapp")
	t.Setenv("HOME", t.TempDir())
	saveDefaultToken(t)
	gitCommitAll(t)
	RepoServerURL = newMockRepoServer(t).URL
	t.Cleanup(func() { RepoServerURL = "" })
	p := &pushCall{}
	p.install(t)
	return p
}

// ---------------------------------- runDeploy 编排（真仓库门控 + 推送桩） ----------------------------------

func TestRunDeploy(t *testing.T) {
	t.Run("deploys to preview", func(t *testing.T) {
		p := setupDeployEnv(t)

		out := captureStdout(t, func() {
			if err := runDeploy("preview", false); err != nil {
				t.Errorf("runDeploy: %v", err)
			}
		})

		if p.cloneURL != "https://repo.example/org/myapp-preview.git" {
			t.Errorf("clone url = %q, want preview repo", p.cloneURL)
		}
		if p.force {
			t.Errorf("force=%v, want false", p.force)
		}
		if p.token == "" {
			t.Error("token should not be empty")
		}
		if !strings.Contains(out, "Deployed 'myapp' to preview") {
			t.Errorf("output missing success line: %q", out)
		}
	})

	t.Run("passes production env and force", func(t *testing.T) {
		p := setupDeployEnv(t)

		_ = captureStdout(t, func() {
			if err := runDeploy("production", true); err != nil {
				t.Errorf("runDeploy: %v", err)
			}
		})

		if p.cloneURL != "https://repo.example/org/myapp-production.git" {
			t.Errorf("clone url = %q, want production repo", p.cloneURL)
		}
		if !p.force {
			t.Errorf("force=%v, want true", p.force)
		}
	})

	t.Run("reads app key from app.yaml", func(t *testing.T) {
		// 工程目录名是随机临时名，部署 key 取自 app.yaml 的 fromdsl
		enterAppDir(t, "fromdsl")
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		gitCommitAll(t)
		RepoServerURL = newMockRepoServer(t).URL
		t.Cleanup(func() { RepoServerURL = "" })
		p := &pushCall{}
		p.install(t)

		out := captureStdout(t, func() {
			if err := runDeploy("preview", false); err != nil {
				t.Errorf("runDeploy: %v", err)
			}
		})

		if !p.called {
			t.Error("expected push to be called")
		}
		if !strings.Contains(out, "Deployed 'fromdsl' to preview") {
			t.Errorf("expected app key from app.yaml in output, got: %q", out)
		}
	})

	t.Run("rejects invalid env", func(t *testing.T) {
		p := setupDeployEnv(t)

		if err := runDeploy("staging", false); err == nil {
			t.Fatal("expected error for invalid env")
		}
		if p.called {
			t.Error("push should not run on invalid env")
		}
	})

	t.Run("fails when app.yaml missing", func(t *testing.T) {
		chdir(t, t.TempDir()) // 干净目录，无 apps/dsl/app.yaml

		if err := runDeploy("preview", false); err == nil {
			t.Fatal("expected error when app.yaml is missing")
		}
	})

	t.Run("fails when app.yaml has invalid key", func(t *testing.T) {
		enterAppDir(t, "_bad") // 下划线开头，validResourceKey 拒绝

		if err := runDeploy("preview", false); err == nil {
			t.Fatal("expected error for invalid key in app.yaml")
		}
	})

	t.Run("fails fast when no git repository", func(t *testing.T) {
		enterAppDir(t, "myapp") // 未 init
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		RepoServerURL = noNetRepoServer(t).URL
		t.Cleanup(func() { RepoServerURL = "" })
		p := &pushCall{}
		p.install(t)

		err := runDeploy("preview", false)
		if err == nil {
			t.Fatal("expected error when no git repository")
		}
		if !strings.Contains(err.Error(), "app init") {
			t.Errorf("error should guide to `app init`, got: %v", err)
		}
		if p.called {
			t.Error("push must not run without a repository")
		}
	})

	t.Run("fails fast when nothing committed", func(t *testing.T) {
		enterAppDir(t, "myapp")
		if _, err := initGitRepo("."); err != nil { // init 但不 commit
			t.Fatal(err)
		}
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		RepoServerURL = noNetRepoServer(t).URL
		t.Cleanup(func() { RepoServerURL = "" })
		p := &pushCall{}
		p.install(t)

		err := runDeploy("preview", false)
		if err == nil {
			t.Fatal("expected error when nothing committed")
		}
		if !strings.Contains(err.Error(), "commit") {
			t.Errorf("error should ask to commit, got: %v", err)
		}
		if p.called {
			t.Error("push must not run with no commits")
		}
	})

	t.Run("fails fast when working tree is dirty", func(t *testing.T) {
		enterAppDir(t, "myapp")
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		gitCommitAll(t)
		writeTestFile(t, "uncommitted.txt", []byte("dirty")) // 提交后再造未跟踪改动
		RepoServerURL = noNetRepoServer(t).URL
		t.Cleanup(func() { RepoServerURL = "" })
		p := &pushCall{}
		p.install(t)

		err := runDeploy("preview", false)
		if err == nil {
			t.Fatal("expected error when working tree is dirty")
		}
		if !strings.Contains(err.Error(), "uncommitted") {
			t.Errorf("error should mention uncommitted changes, got: %v", err)
		}
		if p.called {
			t.Error("push must not run with a dirty tree")
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		enterAppDir(t, "myapp")
		t.Setenv("HOME", t.TempDir())
		gitCommitAll(t) // 本地门控通过，才走到凭证检查
		p := &pushCall{}
		p.install(t)

		if err := runDeploy("preview", false); err == nil {
			t.Fatal("expected error for missing credentials")
		}
		if p.called {
			t.Error("push should not run without credentials")
		}
	})

	t.Run("fails on repository API error", func(t *testing.T) {
		enterAppDir(t, "myapp")
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		gitCommitAll(t)
		srv := newMockMeta(t, 500, "repository could not be prepared")
		t.Cleanup(srv.Close)
		RepoServerURL = srv.URL
		t.Cleanup(func() { RepoServerURL = "" })
		(&pushCall{}).install(t)

		if err := runDeploy("preview", false); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails when env clone url missing", func(t *testing.T) {
		enterAppDir(t, "myapp")
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		gitCommitAll(t)
		srv := newMockMeta(t, 200, "ok") // data 为空 → 无 cloneUrl
		t.Cleanup(srv.Close)
		RepoServerURL = srv.URL
		t.Cleanup(func() { RepoServerURL = "" })
		(&pushCall{}).install(t)

		if err := runDeploy("preview", false); err == nil {
			t.Fatal("expected error when clone url missing")
		}
	})

	t.Run("propagates push error", func(t *testing.T) {
		p := setupDeployEnv(t)
		p.err = errors.New("push rejected")

		var err error
		_ = captureStdout(t, func() { err = runDeploy("preview", false) })
		if err == nil {
			t.Fatal("expected push error to propagate")
		}
	})
}

// ---------------------------------- pushCurrentHead 真实 go-git（本地裸仓库做 remote） ----------------------------------

func TestPushCurrentHead(t *testing.T) {
	t.Run("pushes committed HEAD to dev branch", func(t *testing.T) {
		work := t.TempDir()
		chdir(t, work)
		t.Setenv("HOME", t.TempDir())
		writeTestFile(t, filepath.Join(work, "code.txt"), []byte("v1"))
		gitCommitAll(t)
		bare := newBareRemote(t)

		repo, err := git.PlainOpen(work)
		if err != nil {
			t.Fatal(err)
		}
		if err := pushCurrentHead(repo, bare, "", false); err != nil {
			t.Fatalf("pushCurrentHead: %v", err)
		}

		tree := devTree(t, bare)
		if _, err := tree.File("code.txt"); err != nil {
			t.Errorf("code.txt not pushed to dev: %v", err)
		}
	})

	t.Run("clean redeploy is an up-to-date no-op", func(t *testing.T) {
		work := t.TempDir()
		chdir(t, work)
		t.Setenv("HOME", t.TempDir())
		writeTestFile(t, filepath.Join(work, "code.txt"), []byte("v1"))
		gitCommitAll(t)
		bare := newBareRemote(t)
		repo, err := git.PlainOpen(work)
		if err != nil {
			t.Fatal(err)
		}

		if err := pushCurrentHead(repo, bare, "", false); err != nil {
			t.Fatalf("first push: %v", err)
		}
		// 无任何新提交，再次推送应成功（远端已是该提交 → up-to-date）
		if err := pushCurrentHead(repo, bare, "", false); err != nil {
			t.Errorf("clean redeploy should succeed, got: %v", err)
		}
	})

	t.Run("push after a new commit updates dev", func(t *testing.T) {
		work := t.TempDir()
		chdir(t, work)
		t.Setenv("HOME", t.TempDir())
		codePath := filepath.Join(work, "code.txt")
		writeTestFile(t, codePath, []byte("v1"))
		gitCommitAll(t)
		bare := newBareRemote(t)
		repo, err := git.PlainOpen(work)
		if err != nil {
			t.Fatal(err)
		}

		if err := pushCurrentHead(repo, bare, "", false); err != nil {
			t.Fatalf("first push: %v", err)
		}
		writeTestFile(t, codePath, []byte("v2")) // 用户改并提交，再推
		if _, err := stageAndCommit(repo, "v2"); err != nil {
			t.Fatal(err)
		}
		if err := pushCurrentHead(repo, bare, "", false); err != nil {
			t.Fatalf("second push: %v", err)
		}

		f, err := devTree(t, bare).File("code.txt")
		if err != nil {
			t.Fatalf("code.txt missing on dev: %v", err)
		}
		content, err := f.Contents()
		if err != nil {
			t.Fatal(err)
		}
		if content != "v2" {
			t.Errorf("dev has %q, want v2", content)
		}
	})
}

// newBareRemote 建一个临时裸仓库作为本地 push 目标（file transport，无需网络）
func newBareRemote(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := git.PlainInit(dir, true); err != nil {
		t.Fatal(err)
	}
	return dir
}

// devTree 取裸仓库 deployBranch 分支最新提交的文件树
func devTree(t *testing.T, bareDir string) *object.Tree {
	t.Helper()
	r, err := git.PlainOpen(bareDir)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := r.Reference(plumbing.NewBranchReferenceName(deployBranch), true)
	if err != nil {
		t.Fatalf("dev branch missing on remote: %v", err)
	}
	c, err := r.CommitObject(ref.Hash())
	if err != nil {
		t.Fatal(err)
	}
	tree, err := c.Tree()
	if err != nil {
		t.Fatal(err)
	}
	return tree
}
