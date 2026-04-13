/**
 * [INPUT]: 依赖 cmd 包内的 runConfigureVerify/verifyResult（包内白盒），internal/config、encoding/json、net/http、net/http/httptest
 * [OUTPUT]: 覆盖 configure verify 子命令核心逻辑的单元测试
 * [POS]: cmd 模块 configure_verify.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qfeius/makecli/internal/config"
)

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
		ServerURL = srv.URL

		out := captureStdout(t, func() {
			// runConfigureVerify 在 valid=true 时不调用 os.Exit
			_, _ = runConfigureVerify("default", outputTable)
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
		ServerURL = srv.URL

		out := captureStdout(t, func() {
			_, _ = runConfigureVerify("default", outputJSON)
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
		ServerURL = "http://unused"

		out := captureStdout(t, func() {
			_, _ = runConfigureVerify("default", outputJSON)
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
		ServerURL = "http://unused"
		// 写入非 JWT 格式 token
		if err := config.Save(config.Credentials{
			"default": config.Profile{AccessToken: "not-a-jwt"},
		}); err != nil {
			t.Fatal(err)
		}

		out := captureStdout(t, func() {
			_, _ = runConfigureVerify("default", outputJSON)
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

	t.Run("server returns 401", func(t *testing.T) {
		srv := new401Server()
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		out := captureStdout(t, func() {
			_, _ = runConfigureVerify("default", outputJSON)
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
		ServerURL = srv.URL

		// 写入 config
		if err := config.SaveConfig(config.Config{
			"default": config.ConfigProfile{
				ServerURL:  "https://example.com",
				XTenantID:  "t-123",
				OperatorID: "op-456",
			},
		}); err != nil {
			t.Fatal(err)
		}

		out := captureStdout(t, func() {
			_, _ = runConfigureVerify("default", outputJSON)
		})

		var r verifyResult
		if err := json.Unmarshal([]byte(out), &r); err != nil {
			t.Fatalf("json unmarshal: %v", err)
		}
		if r.ServerURL != "https://example.com" {
			t.Errorf("expected server_url, got: %s", r.ServerURL)
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
		ServerURL = "http://unused"

		out := captureStdout(t, func() {
			_, _ = runConfigureVerify("nonexistent", outputJSON)
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
