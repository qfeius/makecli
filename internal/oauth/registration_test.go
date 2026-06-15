package oauth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterClient(t *testing.T) {
	var gotBody ClientRegistrationRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		_, _ = w.Write([]byte(`{"client_id": "client-123"}`))
	}))
	defer srv.Close()

	resp, err := RegisterClient(context.Background(), srv.Client(), srv.URL, ClientRegistrationRequest{
		ClientName:    "makecli",
		RedirectURIs:  []string{"http://127.0.0.1:54321/callback"},
		GrantTypes:    []string{"authorization_code"},
		ResponseTypes: []string{"code"},
	})
	if err != nil {
		t.Fatalf("RegisterClient: %v", err)
	}
	if resp.ClientID != "client-123" {
		t.Errorf("client_id = %q, want client-123", resp.ClientID)
	}
	if gotBody.ClientName != "makecli" {
		t.Errorf("request client_name = %q", gotBody.ClientName)
	}
	if len(gotBody.RedirectURIs) != 1 || gotBody.RedirectURIs[0] != "http://127.0.0.1:54321/callback" {
		t.Errorf("request redirect_uris = %v", gotBody.RedirectURIs)
	}
}

func TestRegisterClientMissingID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	_, err := RegisterClient(context.Background(), srv.Client(), srv.URL, ClientRegistrationRequest{})
	if err == nil {
		t.Fatal("expected error when client_id is missing")
	}
	if !strings.Contains(err.Error(), "client_id") {
		t.Errorf("error = %v, want mention of client_id", err)
	}
}
