/**
 * [INPUT]: 依赖 cmd 包内的 runWhoami / loginFunc（包内白盒）与 stubMetaServer / saveDefaultToken / setProfile / captureStdout / captureStderr 测试辅助，internal/api、internal/config、encoding/json、errors、net/http、net/http/httptest、strings、testing、time
 * [OUTPUT]: 覆盖 whoami 子命令核心逻辑的单元测试
 * [POS]: cmd 模块 whoami.go 的配套测试，用 httptest 隔离网络、loginFunc 打桩隔离 OAuth 流程
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/qfeius/makecli/internal/api"
	"github.com/qfeius/makecli/internal/config"
)

// stubLogin 临时替换 loginFunc，返回调用计数指针，t.Cleanup 自动还原。
func stubLogin(t *testing.T, fn func() error) *int {
	t.Helper()
	calls := 0
	old := loginFunc
	loginFunc = func(_ time.Duration, _ bool) error {
		calls++
		return fn()
	}
	t.Cleanup(func() { loginFunc = old })
	return &calls
}

// saveToken 在当前 HOME 下把任意 token 写入 default profile。
func saveToken(t *testing.T, token string) {
	t.Helper()
	if err := config.Save(config.Credentials{
		"default": config.Profile{AccessToken: token},
	}); err != nil {
		t.Fatal(err)
	}
}

// newUserInfoServer 起一个 /user/v1/info mock：Authorization 命中 acceptToken 回放用户信息，
// 否则回业务码 401（接口文档「未登录」约定）。
func newUserInfoServer(t *testing.T, acceptToken string, valid bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/make/user/v1/info" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("unexpected method: %s", r.Method)
		}
		if r.Header.Get("Authorization") != "Bearer "+acceptToken {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 401, "msg": "未登录", "data": nil})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200, "msg": "成功",
			"data": map[string]any{
				"id":   "1000000000000000001",
				"name": "test-user",
				"tenant": map[string]any{
					"id": "1000", "tenantName": "示例租户",
				},
				"valid": valid,
			},
		})
	}))
}

func TestRunWhoami(t *testing.T) {
	t.Run("logged in prints identity without login", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		srv := newUserInfoServer(t, "eyJ0eXAiOiJKV1QifQ.eyJzdWIiOiJ0ZXN0In0.c2lnbmF0dXJl", true)
		defer srv.Close()
		stubMetaServer(t, srv.URL)
		calls := stubLogin(t, func() error {
			t.Error("login must not be triggered with a valid token")
			return nil
		})

		var runErr error
		out := captureStdout(t, func() { runErr = runWhoami(outputTable) })
		if runErr != nil {
			t.Fatalf("runWhoami: %v", runErr)
		}
		for _, want := range []string{"logged in as test-user", "1000000000000000001", "示例租户", "FIELD", "VALUE"} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q, got:\n%s", want, out)
			}
		}
		if *calls != 0 {
			t.Errorf("expected 0 login calls, got %d", *calls)
		}
	})

	t.Run("not logged in triggers login then prints identity", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		srv := newUserInfoServer(t, "fresh-token", true)
		defer srv.Close()
		stubMetaServer(t, srv.URL)
		calls := stubLogin(t, func() error {
			saveToken(t, "fresh-token")
			return nil
		})

		var runErr error
		var out string
		errOut := captureStderr(t, func() {
			out = captureStdout(t, func() { runErr = runWhoami(outputTable) })
		})
		if runErr != nil {
			t.Fatalf("runWhoami: %v", runErr)
		}
		if *calls != 1 {
			t.Errorf("expected 1 login call, got %d", *calls)
		}
		if !strings.Contains(errOut, "not logged in") {
			t.Errorf("expected not-logged-in notice on stderr, got: %s", errOut)
		}
		if !strings.Contains(out, "logged in as test-user") {
			t.Errorf("expected identity output, got: %s", out)
		}
	})

	t.Run("expired token triggers re-login and retry", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveToken(t, "stale-token")
		srv := newUserInfoServer(t, "fresh-token", true)
		defer srv.Close()
		stubMetaServer(t, srv.URL)
		calls := stubLogin(t, func() error {
			saveToken(t, "fresh-token")
			return nil
		})

		var runErr error
		var out string
		errOut := captureStderr(t, func() {
			out = captureStdout(t, func() { runErr = runWhoami(outputTable) })
		})
		if runErr != nil {
			t.Fatalf("runWhoami: %v", runErr)
		}
		if *calls != 1 {
			t.Errorf("expected 1 login call, got %d", *calls)
		}
		if !strings.Contains(errOut, "invalid or expired") {
			t.Errorf("expected expiry notice on stderr, got: %s", errOut)
		}
		if !strings.Contains(out, "logged in as test-user") {
			t.Errorf("expected identity output, got: %s", out)
		}
	})

	t.Run("auth failure after re-login surfaces error without looping", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveToken(t, "stale-token")
		srv := newUserInfoServer(t, "never-issued", true)
		defer srv.Close()
		stubMetaServer(t, srv.URL)
		calls := stubLogin(t, func() error {
			saveToken(t, "still-bad-token")
			return nil
		})

		var runErr error
		captureStderr(t, func() {
			_ = captureStdout(t, func() { runErr = runWhoami(outputTable) })
		})
		if !errors.Is(runErr, api.ErrAuthFailed) {
			t.Fatalf("expected ErrAuthFailed, got: %v", runErr)
		}
		if *calls != 1 {
			t.Errorf("expected exactly 1 login call, got %d", *calls)
		}
	})

	t.Run("login failure propagates", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		calls := stubLogin(t, func() error { return errors.New("authorization timed out") })

		var runErr error
		captureStderr(t, func() { runErr = runWhoami(outputTable) })
		if runErr == nil || !strings.Contains(runErr.Error(), "authorization timed out") {
			t.Fatalf("expected login error, got: %v", runErr)
		}
		if *calls != 1 {
			t.Errorf("expected 1 login call, got %d", *calls)
		}
	})

	t.Run("valid=false warns on stderr", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		srv := newUserInfoServer(t, "eyJ0eXAiOiJKV1QifQ.eyJzdWIiOiJ0ZXN0In0.c2lnbmF0dXJl", false)
		defer srv.Close()
		stubMetaServer(t, srv.URL)
		stubLogin(t, func() error { return nil })

		var runErr error
		errOut := captureStderr(t, func() {
			_ = captureStdout(t, func() { runErr = runWhoami(outputTable) })
		})
		if runErr != nil {
			t.Fatalf("runWhoami: %v", runErr)
		}
		if !strings.Contains(errOut, "valid=false") {
			t.Errorf("expected validity warning on stderr, got: %s", errOut)
		}
	})

	t.Run("prints json when requested", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		srv := newUserInfoServer(t, "eyJ0eXAiOiJKV1QifQ.eyJzdWIiOiJ0ZXN0In0.c2lnbmF0dXJl", true)
		defer srv.Close()
		stubMetaServer(t, srv.URL)
		stubLogin(t, func() error { return nil })

		var runErr error
		out := captureStdout(t, func() { runErr = runWhoami(outputJSON) })
		if runErr != nil {
			t.Fatalf("runWhoami: %v", runErr)
		}
		var info api.UserInfo
		if err := json.Unmarshal([]byte(out), &info); err != nil {
			t.Fatalf("invalid JSON output: %v\n%s", err, out)
		}
		if info.Name != "test-user" || info.Tenant.ID != "1000" || !info.Valid {
			t.Errorf("unexpected JSON payload: %+v", info)
		}
	})

	t.Run("rejects invalid output format", func(t *testing.T) {
		if err := runWhoami("yaml"); err == nil || !strings.Contains(err.Error(), "unsupported output format") {
			t.Fatalf("expected format error, got: %v", err)
		}
	})

	t.Run("rejects reserved profile name", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		setProfile(t, "settings")
		if err := runWhoami(outputTable); err == nil {
			t.Fatal("expected reserved profile error")
		}
	})
}
