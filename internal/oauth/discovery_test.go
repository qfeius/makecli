package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
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

	if _, err := Discover(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Error("expected error on 500 response")
	}
}
