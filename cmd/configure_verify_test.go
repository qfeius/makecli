/**
 * [INPUT]: 依赖 cmd 包内的 runConfigureVerify/verifyResult（包内白盒），internal/config、encoding/json、net/http、net/http/httptest
 * [OUTPUT]: 覆盖 configure verify 子命令核心逻辑的单元测试
 * [POS]: cmd 模块 configure_verify.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/qfeius/makecli/internal/config"
)

// fakeJWTWithClaims 构造结构合法（三段 base64url）、payload 携带指定 claims 的测试 JWT
func fakeJWTWithClaims(t *testing.T, claims map[string]any) string {
	t.Helper()
	body, err := json.Marshal(claims)
	if err != nil {
		t.Fatal(err)
	}
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(`{"alg":"RS256","typ":"JWT"}`)) + "." + enc(body) + "." + enc([]byte("signature"))
}

// saveTokenWithClaims 在当前 HOME 下写入 default profile 的指定 claims 测试 JWT
func saveTokenWithClaims(t *testing.T, claims map[string]any) {
	t.Helper()
	if err := config.Save(config.Credentials{
		"default": config.Profile{AccessToken: fakeJWTWithClaims(t, claims)},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestRunConfigureVerify(t *testing.T) {
	// 构建一个正常响应的 mock server
	newVerifyServer := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "message": "success",
				"data":       []any{},
				"pagination": map[string]any{"total": 0},
			})
		}))
	}

	// 构建一个返回 401 的 mock server
	new401Server := func() *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 401, "msg": "unauthorized",
			})
		}))
	}

	t.Run("valid token table output", func(t *testing.T) {
		srv := newVerifyServer()
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		out := captureStdout(t, func() {
			// runConfigureVerify 在 valid=true 时不调用 os.Exit
			_, _ = runConfigureVerify(outputTable)
		})
		if !strings.Contains(out, "ok") {
			t.Errorf("expected 'ok' in output, got: %s", out)
		}
	})

	t.Run("valid token json output", func(t *testing.T) {
		srv := newVerifyServer()
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		out := captureStdout(t, func() {
			_, _ = runConfigureVerify(outputJSON)
		})

		var r verifyResult
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatalf("json unmarshal: %v", err)
		}
		if !r.Valid {
			t.Errorf("expected valid=true, got false")
		}
		if r.Message != "ok" {
			t.Errorf("expected message 'ok', got: %s", r.Message)
		}
		if r.Token == "" {
			t.Errorf("expected masked token in output")
		}
	})

	t.Run("token not configured", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		MetaServerURL = "http://unused"

		out := captureStdout(t, func() {
			_, _ = runConfigureVerify(outputJSON)
		})

		var r verifyResult
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatalf("json unmarshal: %v", err)
		}
		if r.Valid {
			t.Errorf("expected valid=false")
		}
		if r.Message != "token not configured" {
			t.Errorf("expected 'token not configured', got: %s", r.Message)
		}
	})

	t.Run("malformed JWT", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		MetaServerURL = "http://unused"
		// 写入非 JWT 格式 token
		if err := config.Save(config.Credentials{
			"default": config.Profile{AccessToken: "not-a-jwt"},
		}); err != nil {
			t.Fatal(err)
		}

		out := captureStdout(t, func() {
			_, _ = runConfigureVerify(outputJSON)
		})

		var r verifyResult
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatalf("json unmarshal: %v", err)
		}
		if r.Valid {
			t.Errorf("expected valid=false")
		}
		if !strings.Contains(r.Message, "malformed JWT") {
			t.Errorf("expected 'malformed JWT' in message, got: %s", r.Message)
		}
	})

	t.Run("expired JWT rejected locally without network", func(t *testing.T) {
		// 探测端点返回 200 也不能救过期 token（复现线上误报场景），且请求根本不该发出
		hit := false
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			hit = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "message": "success",
				"data":       []any{},
				"pagination": map[string]any{"total": 0},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		issued := time.Now().Add(-48 * time.Hour)
		expired := time.Now().Add(-24 * time.Hour)
		saveTokenWithClaims(t, map[string]any{"iat": issued.Unix(), "exp": expired.Unix()})
		MetaServerURL = srv.URL

		out := captureStdout(t, func() {
			_, _ = runConfigureVerify(outputJSON)
		})

		var r verifyResult
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatalf("json unmarshal: %v", err)
		}
		if r.Valid {
			t.Errorf("expected valid=false for expired token")
		}
		if !strings.Contains(r.Message, "token expired. `makecli login --profile default` to renew token") {
			t.Errorf("expected renew guidance with profile in message, got: %s", r.Message)
		}
		if r.IssuedAt != issued.Format(time.RFC3339) {
			t.Errorf("expected issued_at %s, got: %s", issued.Format(time.RFC3339), r.IssuedAt)
		}
		if r.ExpiresAt != expired.Format(time.RFC3339) {
			t.Errorf("expected expires_at %s, got: %s", expired.Format(time.RFC3339), r.ExpiresAt)
		}
		if hit {
			t.Errorf("expected no network request for expired token")
		}
	})

	t.Run("valid token json includes issued_at and expires_at", func(t *testing.T) {
		srv := newVerifyServer()
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		issued := time.Now().Add(-time.Hour)
		expires := time.Now().Add(24 * time.Hour)
		saveTokenWithClaims(t, map[string]any{"iat": issued.Unix(), "exp": expires.Unix()})
		MetaServerURL = srv.URL

		out := captureStdout(t, func() {
			_, _ = runConfigureVerify(outputJSON)
		})

		var r verifyResult
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatalf("json unmarshal: %v", err)
		}
		if !r.Valid {
			t.Errorf("expected valid=true, got false: %s", r.Message)
		}
		if r.IssuedAt != issued.Format(time.RFC3339) {
			t.Errorf("expected issued_at %s, got: %s", issued.Format(time.RFC3339), r.IssuedAt)
		}
		if r.ExpiresAt != expires.Format(time.RFC3339) {
			t.Errorf("expected expires_at %s, got: %s", expires.Format(time.RFC3339), r.ExpiresAt)
		}
	})

	t.Run("token without exp claim falls through to online verify", func(t *testing.T) {
		// 无 exp 不下本地结论：在线验证放行则 valid=true，时间字段留空
		srv := newVerifyServer()
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t) // payload 仅含 sub，无 iat/exp
		MetaServerURL = srv.URL

		out := captureStdout(t, func() {
			_, _ = runConfigureVerify(outputJSON)
		})

		var r verifyResult
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatalf("json unmarshal: %v", err)
		}
		if !r.Valid {
			t.Errorf("expected valid=true, got false: %s", r.Message)
		}
		if r.IssuedAt != "" || r.ExpiresAt != "" {
			t.Errorf("expected empty issued_at/expires_at, got: %s / %s", r.IssuedAt, r.ExpiresAt)
		}
	})

	t.Run("server returns 401", func(t *testing.T) {
		srv := new401Server()
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		out := captureStdout(t, func() {
			_, _ = runConfigureVerify(outputJSON)
		})

		var r verifyResult
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatalf("json unmarshal: %v", err)
		}
		if r.Valid {
			t.Errorf("expected valid=false")
		}
		if !strings.Contains(r.Message, "token invalid") {
			t.Errorf("expected 'token invalid' in message, got: %s", r.Message)
		}
	})

	t.Run("json includes config fields", func(t *testing.T) {
		srv := newVerifyServer()
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		// 写入 config
		if err := config.SaveConfig(config.Config{
			"default": config.ConfigProfile{
				MetaServerURL: "https://example.com",
				XTenantID:     "t-123",
				OperatorID:    "op-456",
			},
		}); err != nil {
			t.Fatal(err)
		}

		out := captureStdout(t, func() {
			_, _ = runConfigureVerify(outputJSON)
		})

		var r verifyResult
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatalf("json unmarshal: %v", err)
		}
		if r.MetaServerURL != "https://example.com" {
			t.Errorf("expected meta_server_url, got: %s", r.MetaServerURL)
		}
		if r.TenantID != "t-123" {
			t.Errorf("expected tenant_id, got: %s", r.TenantID)
		}
		if r.OperatorID != "op-456" {
			t.Errorf("expected operator_id, got: %s", r.OperatorID)
		}
	})

	t.Run("unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		MetaServerURL = "http://unused"
		setProfile(t, "nonexistent")

		out := captureStdout(t, func() {
			_, _ = runConfigureVerify(outputJSON)
		})

		var r verifyResult
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatalf("json unmarshal: %v", err)
		}
		if r.Valid {
			t.Errorf("expected valid=false")
		}
		if r.Profile != "nonexistent" {
			t.Errorf("expected profile 'nonexistent', got: %s", r.Profile)
		}
	})
}
