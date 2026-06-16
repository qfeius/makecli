/**
 * [INPUT]: 依赖 cmd 包内的 runLogin / openBrowserFunc / authMetadataURL（包内白盒），internal/config、encoding/json、net/http、net/http/httptest、net/url、strings、testing、time
 * [OUTPUT]: 覆盖 login 子命令编排逻辑的单元测试
 * [POS]: cmd 模块 login.go 的配套测试，用 httptest 模拟 OAuth 端点 + openBrowserFunc 桩注入回调，t.Setenv 隔离凭证
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/qfeius/makecli/internal/config"
)

// newOAuthServer 起一个同时服务 metadata/register/token 三端点的 mock。
// metadata 端点用 r.Host 拼出绝对 endpoint，避免 server URL 的先有蛋问题。
func newOAuthServer(t *testing.T, withRegistration bool, token string) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/oauth-authorization-server/make", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		meta := map[string]any{
			"issuer":                 base,
			"authorization_endpoint": base + "/authorize",
			"token_endpoint":         base + "/token",
		}
		if withRegistration {
			meta["registration_endpoint"] = base + "/register"
		}
		_ = json.NewEncoder(w).Encode(meta)
	})
	mux.HandleFunc("/register", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": "test-client"})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": token, "token_type": "Bearer", "expires_in": 3600,
		})
	})
	return httptest.NewServer(mux)
}

// stubBrowserInjectCallback 把 openBrowserFunc 换成「解析 authURL 拿 redirect_uri+state，
// 直接 GET 回调注入授权码」，模拟 IdP 重定向回本地，免真浏览器。
func stubBrowserInjectCallback(t *testing.T, code string) {
	t.Helper()
	old := openBrowserFunc
	openBrowserFunc = func(authURL string) error {
		u, err := url.Parse(authURL)
		if err != nil {
			return err
		}
		q := u.Query()
		resp, err := http.Get(q.Get("redirect_uri") + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(q.Get("state")))
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		return nil
	}
	t.Cleanup(func() { openBrowserFunc = old })
}

// setAuthBaseURL 把当前 Profile 的 auth-server-url 指向 httptest，派生的 .well-known 路径正好命中 mock。
// 须在 t.Setenv("HOME",...) 之后、且 setProfile 之后调用（写入对应 profile）。
func setAuthBaseURL(t *testing.T, srv *httptest.Server) {
	t.Helper()
	cfg, err := config.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	p := cfg[Profile]
	p.AuthServerURL = srv.URL
	cfg[Profile] = p
	if err := config.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
}

func TestRunLoginSuccess(t *testing.T) {
	fakeToken := "eyJ0eXAiOiJKV1QifQ.eyJzdWIiOiJ0ZXN0In0.c2lnbmF0dXJl"
	srv := newOAuthServer(t, true, fakeToken)
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	setAuthBaseURL(t, srv)
	stubBrowserInjectCallback(t, "auth-code-1")

	var runErr error
	out := captureStdout(t, func() {
		runErr = runLogin(5*time.Second, false)
	})
	if runErr != nil {
		t.Fatalf("runLogin: %v", runErr)
	}
	if !strings.Contains(out, "Login succeeded") {
		t.Errorf("expected success message, got: %s", out)
	}

	creds, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if creds["default"].AccessToken != fakeToken {
		t.Errorf("token = %q, want %q", creds["default"].AccessToken, fakeToken)
	}
}

func TestRunLoginWritesToSelectedProfile(t *testing.T) {
	fakeToken := "eyJ0eXAiOiJKV1QifQ.eyJzdWIiOiJ0ZXN0In0.c2lnbmF0dXJl"
	srv := newOAuthServer(t, true, fakeToken)
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	setProfile(t, "todo")
	setAuthBaseURL(t, srv)
	stubBrowserInjectCallback(t, "auth-code-1")

	var runErr error
	_ = captureStdout(t, func() {
		runErr = runLogin(5*time.Second, false)
	})
	if runErr != nil {
		t.Fatalf("runLogin: %v", runErr)
	}

	creds, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if creds["todo"].AccessToken != fakeToken {
		t.Errorf("[todo] token = %q, want %q", creds["todo"].AccessToken, fakeToken)
	}
}

func TestRunLoginNoOpenBrowserPrintsURLAndTimesOut(t *testing.T) {
	srv := newOAuthServer(t, true, "unused")
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	setAuthBaseURL(t, srv)
	// openBrowserFunc not stubbed: no callback ever arrives -> Wait times out.

	var runErr error
	out := captureStdout(t, func() {
		runErr = runLogin(200*time.Millisecond, true)
	})
	if runErr == nil {
		t.Error("expected timeout error in no-open-browser mode")
	}
	if !strings.Contains(out, "/authorize") {
		t.Errorf("expected authorization URL printed, got: %s", out)
	}
}

func TestRunLoginMissingRegistrationEndpoint(t *testing.T) {
	srv := newOAuthServer(t, false, "unused") // no registration_endpoint
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	setAuthBaseURL(t, srv)

	err := runLogin(5*time.Second, false)
	if err == nil {
		t.Fatal("expected error when registration_endpoint is absent")
	}
	if !strings.Contains(err.Error(), "registration_endpoint") {
		t.Errorf("error = %v, want mention of registration_endpoint", err)
	}
}

func TestAuthMetadataURL(t *testing.T) {
	// 基址已由调用方按 flag>profile>env 解析；本函数只负责拼 .well-known 路径 + 裁末尾斜杠。
	cases := []struct{ authBase, want string }{
		{"https://test-myaccount.qtech.cn", "https://test-myaccount.qtech.cn/.well-known/oauth-authorization-server/make"},
		{"https://test-myaccount.qtech.cn/", "https://test-myaccount.qtech.cn/.well-known/oauth-authorization-server/make"},
	}
	for _, c := range cases {
		if got := authMetadataURL(c.authBase); got != c.want {
			t.Errorf("authMetadataURL(%q) = %q, want %q", c.authBase, got, c.want)
		}
	}
}
