/**
 * [INPUT]: 依赖 cmd 包内的 runDeploy / gitOutputFunc / gitPushFunc（包内白盒），encoding/json、errors、fmt、net/http、net/http/httptest、strings、testing
 * [OUTPUT]: 覆盖 deploy 子命令核心逻辑的单元测试（环境校验 / appKey 推断 / cloneUrl 选取 / git push 参数 / 错误路径）
 * [POS]: cmd 模块 deploy.go 的配套测试，用 httptest 隔离网络、包级函数变量打桩隔离真实 git 进程
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stubGit 打桩 git 交互：rev-parse 按 toplevel/sha 返回，push 记录调用参数。
// toplevelErr 非 nil 时模拟「当前目录不是 git 仓库」。
type stubGit struct {
	toplevel    string
	toplevelErr error
	pushURL     string
	pushToken   string
	pushForce   bool
	pushErr     error
	pushCalled  bool
}

func (s *stubGit) install(t *testing.T) {
	t.Helper()
	oldOutput, oldPush := gitOutputFunc, gitPushFunc
	gitOutputFunc = func(args ...string) (string, error) {
		joined := strings.Join(args, " ")
		switch joined {
		case "rev-parse --show-toplevel":
			return s.toplevel, s.toplevelErr
		case "rev-parse --short HEAD":
			return "abc1234", nil
		}
		return "", fmt.Errorf("unexpected git args: %s", joined)
	}
	gitPushFunc = func(cloneURL, token string, force bool) error {
		s.pushCalled = true
		s.pushURL, s.pushToken, s.pushForce = cloneURL, token, force
		return s.pushErr
	}
	t.Cleanup(func() { gitOutputFunc, gitPushFunc = oldOutput, oldPush })
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

// setupDeployEnv 准备凭证 + repo server 指向，返回安装好的 git 桩
func setupDeployEnv(t *testing.T) *stubGit {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	saveDefaultToken(t)
	RepoServerURL = newMockRepoServer(t).URL
	t.Cleanup(func() { RepoServerURL = "" })
	git := &stubGit{toplevel: "/work/myapp"}
	git.install(t)
	return git
}

func TestRunDeploy(t *testing.T) {
	t.Run("deploys to preview", func(t *testing.T) {
		git := setupDeployEnv(t)

		out := captureStdout(t, func() {
			if err := runDeploy("preview", "myapp", false); err != nil {
				t.Errorf("runDeploy: %v", err)
			}
		})

		if git.pushURL != "https://repo.example/org/myapp-preview.git" {
			t.Errorf("push url = %q, want preview repo", git.pushURL)
		}
		if git.pushForce {
			t.Errorf("push force = %v, want false", git.pushForce)
		}
		if git.pushToken == "" {
			t.Error("push token should not be empty")
		}
		if !strings.Contains(out, "Deployed 'myapp' to preview") {
			t.Errorf("output missing success line: %q", out)
		}
	})

	t.Run("deploys to production with force", func(t *testing.T) {
		git := setupDeployEnv(t)

		_ = captureStdout(t, func() {
			if err := runDeploy("production", "myapp", true); err != nil {
				t.Errorf("runDeploy: %v", err)
			}
		})

		if git.pushURL != "https://repo.example/org/myapp-production.git" {
			t.Errorf("push url = %q, want production repo", git.pushURL)
		}
		if !git.pushForce {
			t.Errorf("push force = %v, want true", git.pushForce)
		}
	})

	t.Run("infers app key from git toplevel", func(t *testing.T) {
		git := setupDeployEnv(t)

		_ = captureStdout(t, func() {
			if err := runDeploy("preview", "", false); err != nil {
				t.Errorf("runDeploy: %v", err)
			}
		})

		if !git.pushCalled {
			t.Error("expected push to be called with inferred app key")
		}
	})

	t.Run("rejects invalid env", func(t *testing.T) {
		if err := runDeploy("staging", "myapp", false); err == nil {
			t.Fatal("expected error for invalid env")
		}
	})

	t.Run("fails when not a git repository", func(t *testing.T) {
		git := setupDeployEnv(t)
		git.toplevelErr = errors.New("not a git repository")

		if err := runDeploy("preview", "", false); err == nil {
			t.Fatal("expected error when toplevel lookup fails")
		}
	})

	t.Run("fails when inferred key is invalid", func(t *testing.T) {
		git := setupDeployEnv(t)
		git.toplevel = "/work/my-app" // 连字符不是合法 key

		if err := runDeploy("preview", "", false); err == nil {
			t.Fatal("expected error for invalid inferred key")
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		(&stubGit{toplevel: "/work/myapp"}).install(t)

		if err := runDeploy("preview", "myapp", false); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails on repository API error", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		srv := newMockMeta(t, 500, "repository could not be prepared")
		t.Cleanup(srv.Close)
		RepoServerURL = srv.URL
		t.Cleanup(func() { RepoServerURL = "" })
		(&stubGit{toplevel: "/work/myapp"}).install(t)

		if err := runDeploy("preview", "myapp", false); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails when env clone url missing", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		srv := newMockMeta(t, 200, "ok") // data 为空 → 无 cloneUrl
		t.Cleanup(srv.Close)
		RepoServerURL = srv.URL
		t.Cleanup(func() { RepoServerURL = "" })
		(&stubGit{toplevel: "/work/myapp"}).install(t)

		if err := runDeploy("preview", "myapp", false); err == nil {
			t.Fatal("expected error when clone url missing")
		}
	})

	t.Run("fails when git push fails", func(t *testing.T) {
		git := setupDeployEnv(t)
		git.pushErr = errors.New("non-fast-forward")

		_ = captureStdout(t, func() {
			if err := runDeploy("preview", "myapp", false); err == nil {
				t.Error("expected error when push fails")
			}
		})
	})
}
