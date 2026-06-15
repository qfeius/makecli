package oauth

import (
	"bytes"
	"testing"
)

func TestNewCodeVerifier(t *testing.T) {
	// 32 zero bytes -> base64 raw-url is 43 'A's (deterministic, seedable).
	reader := bytes.NewReader(make([]byte, 32))
	got, err := NewCodeVerifier(reader)
	if err != nil {
		t.Fatalf("NewCodeVerifier: %v", err)
	}
	want := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if got != want {
		t.Errorf("verifier = %q, want %q", got, want)
	}
}

func TestNewCodeVerifierShortReader(t *testing.T) {
	if _, err := NewCodeVerifier(bytes.NewReader([]byte{0x01})); err == nil {
		t.Error("expected error on short reader")
	}
}

func TestS256Challenge(t *testing.T) {
	// RFC 7636 Appendix B test vector.
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	if got := S256Challenge(verifier); got != want {
		t.Errorf("challenge = %q, want %q", got, want)
	}
}
