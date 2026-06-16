/**
 * [INPUT]: 依赖 context、net/http、net/http/httptest、strings、testing；包内 Discover（白盒）
 * [OUTPUT]: 覆盖 Discover 的单元测试（成功解析端点 / 500 错误 / 缺 authz·token 端点）
 * [POS]: internal/oauth 模块 discovery.go 的配套测试，用 httptest 隔离网络
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDiscover(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"issuer": "https://idp.example",
			"authorization_endpoint": "https://idp.example/authorize",
			"token_endpoint": "https://idp.example/token",
			"registration_endpoint": "https://idp.example/register"
		}`))
	}))
	defer srv.Close()

	meta, err := Discover(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if meta.AuthorizationEndpoint != "https://idp.example/authorize" {
		t.Errorf("authorization_endpoint = %q", meta.AuthorizationEndpoint)
	}
	if meta.TokenEndpoint != "https://idp.example/token" {
		t.Errorf("token_endpoint = %q", meta.TokenEndpoint)
	}
	if meta.RegistrationEndpoint != "https://idp.example/register" {
		t.Errorf("registration_endpoint = %q", meta.RegistrationEndpoint)
	}
}

func TestDiscoverHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := Discover(context.Background(), srv.Client(), srv.URL)
	if err == nil {
		t.Fatal("expected error on 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error = %v, want mention of status 500", err)
	}
}

func TestDiscoverMissingEndpoints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"issuer":"https://idp.example"}`))
	}))
	defer srv.Close()
	if _, err := Discover(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Error("expected error when authorization_endpoint/token_endpoint are missing")
	}
}
