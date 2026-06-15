package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestBuildAuthorizationURL(t *testing.T) {
	got, err := BuildAuthorizationURL(AuthorizationRequest{
		AuthorizationEndpoint: "https://idp.example/authorize",
		BusinessType:          "make",
		ClientID:              "client-123",
		RedirectURL:           "http://127.0.0.1:5000/callback",
		Resource:              "",
		Scopes:                []string{"mcp:tools", "mcp:resources"},
		State:                 "state-xyz",
		CodeChallenge:         "challenge-abc",
	})
	if err != nil {
		t.Fatalf("BuildAuthorizationURL: %v", err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	q := u.Query()
	checks := map[string]string{
		"business_type":         "make",
		"client_id":             "client-123",
		"code_challenge":        "challenge-abc",
		"code_challenge_method": "S256",
		"redirect_uri":          "http://127.0.0.1:5000/callback",
		"response_type":         "code",
		"scope":                 "mcp:tools mcp:resources",
		"state":                 "state-xyz",
	}
	for k, want := range checks {
		if q.Get(k) != want {
			t.Errorf("query %s = %q, want %q", k, q.Get(k), want)
		}
	}
	// resource is empty -> must NOT be present
	if _, ok := q["resource"]; ok {
		t.Error("resource should be omitted when empty")
	}
}

func TestExchangeAuthorizationCode(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		_, _ = w.Write([]byte(`{"access_token":"tok-abc","token_type":"Bearer","expires_in":3600}`))
	}))
	defer srv.Close()

	token, err := ExchangeAuthorizationCode(context.Background(), srv.Client(), TokenExchangeRequest{
		TokenEndpoint: srv.URL,
		ClientID:      "client-123",
		Code:          "code-xyz",
		CodeVerifier:  "verifier-xyz",
		RedirectURL:   "http://127.0.0.1:5000/callback",
	})
	if err != nil {
		t.Fatalf("ExchangeAuthorizationCode: %v", err)
	}
	if token.AccessToken != "tok-abc" {
		t.Errorf("access_token = %q", token.AccessToken)
	}
	if token.Expiry.IsZero() {
		t.Error("expected non-zero expiry from expires_in")
	}
	if gotForm.Get("grant_type") != "authorization_code" {
		t.Errorf("grant_type = %q", gotForm.Get("grant_type"))
	}
	if gotForm.Get("code") != "code-xyz" {
		t.Errorf("code = %q", gotForm.Get("code"))
	}
	if gotForm.Get("code_verifier") != "verifier-xyz" {
		t.Errorf("code_verifier = %q", gotForm.Get("code_verifier"))
	}
}

func TestExchangeAuthorizationCodeHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()

	if _, err := ExchangeAuthorizationCode(context.Background(), srv.Client(), TokenExchangeRequest{TokenEndpoint: srv.URL}); err == nil {
		t.Error("expected error on 400 response")
	}
}

func TestStartCallbackServerEphemeralPort(t *testing.T) {
	cb, redirectURL, err := StartCallbackServer()
	if err != nil {
		t.Fatalf("StartCallbackServer: %v", err)
	}
	defer cb.Close()

	if !strings.HasPrefix(redirectURL, "http://127.0.0.1:") {
		t.Errorf("redirectURL = %q, want loopback http URL", redirectURL)
	}
	if !strings.HasSuffix(redirectURL, "/callback") {
		t.Errorf("redirectURL = %q, want /callback path", redirectURL)
	}
}

func TestCallbackServerWaitSuccess(t *testing.T) {
	cb, redirectURL, err := StartCallbackServer()
	if err != nil {
		t.Fatalf("StartCallbackServer: %v", err)
	}
	defer cb.Close()

	resp, err := http.Get(redirectURL + "?code=code-xyz&state=state-1")
	if err != nil {
		t.Fatalf("callback GET: %v", err)
	}
	_ = resp.Body.Close()

	code, err := cb.Wait(context.Background(), "state-1")
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if code != "code-xyz" {
		t.Errorf("code = %q, want code-xyz", code)
	}
}

func TestCallbackServerWaitStateMismatch(t *testing.T) {
	cb, redirectURL, err := StartCallbackServer()
	if err != nil {
		t.Fatalf("StartCallbackServer: %v", err)
	}
	defer cb.Close()

	resp, err := http.Get(redirectURL + "?code=code-xyz&state=WRONG")
	if err != nil {
		t.Fatalf("callback GET: %v", err)
	}
	_ = resp.Body.Close()

	if _, err := cb.Wait(context.Background(), "state-1"); err == nil {
		t.Error("expected state mismatch error")
	}
}

func TestCallbackServerWaitTimeout(t *testing.T) {
	cb, _, err := StartCallbackServer()
	if err != nil {
		t.Fatalf("StartCallbackServer: %v", err)
	}
	defer cb.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := cb.Wait(ctx, "state-1"); err == nil {
		t.Error("expected timeout error when no callback arrives")
	}
}
