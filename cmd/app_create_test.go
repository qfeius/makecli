/**
 * [INPUT]: 依赖 cmd 包内的 runAppCreate（包内白盒），internal/config、encoding/json、net/http、net/http/httptest
 * [OUTPUT]: 覆盖 app create 子命令核心逻辑的单元测试
 * [POS]: cmd 模块 app_create.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MakeHQ/makecli/internal/config"
)

func TestRunAppCreate(t *testing.T) {
	t.Run("creates app via API", func(t *testing.T) {
		srv := newMockMeta(t, 200, "create app success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		if err := runAppCreate("myapp", "default", srv.URL); err != nil {
			t.Fatalf("runAppCreate: %v", err)
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		// 未写入任何凭证，预期报错
		if err := runAppCreate("myapp", "default", "http://localhost"); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails on API error response", func(t *testing.T) {
		srv := newMockMeta(t, 400, "invalid app name")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		if err := runAppCreate("myapp", "default", srv.URL); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)

		if err := runAppCreate("myapp", "nonexistent", "http://localhost"); err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})
}

// newMockMeta 启动一个返回固定 code/message 的测试 Meta Server
func newMockMeta(t *testing.T, code int, message string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code":    code,
			"message": message,
			"data":    map[string]any{},
		})
	}))
}

// saveDefaultToken 在当前 HOME 下写入 default profile 的测试 JWT
func saveDefaultToken(t *testing.T) {
	t.Helper()
	// 合法 JWT 格式（三段 base64url），validateJWT 校验通过
	fakeToken := "eyJ0eXAiOiJKV1QifQ.eyJzdWIiOiJ0ZXN0In0.c2lnbmF0dXJl"
	if err := config.Save(config.Credentials{
		"default": config.Profile{AccessToken: fakeToken},
	}); err != nil {
		t.Fatal(err)
	}
}
