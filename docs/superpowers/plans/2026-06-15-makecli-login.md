# makecli login Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a top-level `makecli login` command that opens the browser for OAuth login (PKCE) and writes the resulting `access_token` into `~/.make/credentials`.

**Architecture:** A new pure-protocol package `internal/oauth/` (PKCE, single-hop discovery, RFC 7591 dynamic registration, authorization-URL build + token exchange + loopback callback server + browser launcher). A new `cmd/login.go` orchestrates the flow against a hardcoded `make` dev preset and persists only `access_token` via the existing `config.Save`. The loopback callback binds an ephemeral port (`127.0.0.1:0`) and a fresh OAuth client is registered on every login — these two choices eliminate the fixed-port and persisted-client_id special cases.

**Tech Stack:** Go, `github.com/spf13/cobra`, stdlib `net/http` + `net/http/httptest` for tests. Source reference for the port: `deps/contract-cli/internal/oauth/`.

**Reference reading before starting:**
- Spec: `docs/superpowers/specs/2026-06-15-makecli-auth-login-design.md`
- Source primitives being ported: `deps/contract-cli/internal/oauth/{pkce,discovery,registration,login}.go`
- makecli credential store: `internal/config/credentials.go` (`config.Load`/`config.Save`/`config.Credentials`/`config.Profile`)
- Test idioms: `cmd/stdout_test.go` (`captureStdout`, `setProfile`), `cmd/configure_verify_test.go` (httptest + `t.Setenv("HOME", t.TempDir())`), `cmd/deploy.go` (package-var stub pattern)
- **Discipline (CLAUDE.md):** Go toolchain commands fail spuriously under the command sandbox (module cache unwritable) — run `make vet`/`make test`/`golangci-lint` with the sandbox disabled. Verify `exit 0` BEFORE any commit; never batch test+commit in one shot.

---

## File Structure

**Create:**
- `internal/oauth/pkce.go` — code verifier / state / S256 challenge
- `internal/oauth/pkce_test.go`
- `internal/oauth/discovery.go` — `Discover()` → authorization/token/registration endpoints
- `internal/oauth/discovery_test.go`
- `internal/oauth/registration.go` — `RegisterClient()` (RFC 7591)
- `internal/oauth/registration_test.go`
- `internal/oauth/flow.go` — `Token`, `BuildAuthorizationURL`, `ExchangeAuthorizationCode`, `CallbackServer` (ephemeral-port loopback), `OpenBrowser`
- `internal/oauth/flow_test.go`
- `internal/oauth/CLAUDE.md` — L2 module map
- `cmd/login.go` — `newLoginCmd`, `runLogin`, `make` preset constants, `openBrowserFunc` stub var
- `cmd/login_test.go`

**Modify:**
- `cmd/root.go` — register `newLoginCmd()`
- `cmd/CLAUDE.md` — add `login.go` / `login_test.go` to member list
- `CLAUDE.md` (repo root) — add `internal/oauth/` to `<directory>`, add `login` to cmd subcommand list

---

## Task 1: PKCE primitive

**Files:**
- Create: `internal/oauth/pkce.go`
- Test: `internal/oauth/pkce_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/oauth/pkce_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `make test 2>&1 | grep -A2 oauth` or `go test ./internal/oauth/`
Expected: FAIL — `undefined: NewCodeVerifier` / `undefined: S256Challenge` (package doesn't compile yet).

- [ ] **Step 3: Write minimal implementation**

Create `internal/oauth/pkce.go`:

```go
/**
 * [INPUT]: 依赖 crypto/rand、crypto/sha256、encoding/base64、fmt、io
 * [OUTPUT]: 对外提供 NewCodeVerifier / NewState / S256Challenge
 * [POS]: internal/oauth 的 PKCE 原语，被 login 流程生成 code_verifier / state / code_challenge
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
)

// NewCodeVerifier 生成 PKCE code_verifier（32 字节随机 → base64 raw-url）。
// reader 为 nil 时用 crypto/rand，测试可注入确定性 reader。
func NewCodeVerifier(reader io.Reader) (string, error) {
	if reader == nil {
		reader = rand.Reader
	}
	buf := make([]byte, 32)
	if _, err := io.ReadFull(reader, buf); err != nil {
		return "", fmt.Errorf("read random verifier bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// NewState 生成 OAuth state，与 code_verifier 同构（32 字节随机）。
func NewState(reader io.Reader) (string, error) {
	return NewCodeVerifier(reader)
}

// S256Challenge 计算 code_challenge = base64rawurl(sha256(verifier))。
func S256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
```

- [ ] **Step 4: Run test to verify it passes**

Run (sandbox disabled): `go test ./internal/oauth/ -run TestNewCodeVerifier -v` then `go test ./internal/oauth/ -v`
Expected: PASS (all three tests).

- [ ] **Step 5: Commit**

```bash
git add internal/oauth/pkce.go internal/oauth/pkce_test.go
git commit -m "feat(oauth): add PKCE verifier/state/challenge primitives"
```

---

## Task 2: Single-hop discovery

**Files:**
- Create: `internal/oauth/discovery.go`
- Test: `internal/oauth/discovery_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/oauth/discovery_test.go`:

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./internal/oauth/ -run TestDiscover -v`
Expected: FAIL — `undefined: Discover`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/oauth/discovery.go`:

```go
/**
 * [INPUT]: 依赖 context、encoding/json、fmt、io、net/http、strings
 * [OUTPUT]: 对外提供 ServerMetadata 类型、Discover 函数
 * [POS]: internal/oauth 的 OAuth 元数据发现（单跳），把 authorization-server metadata URL 解析为 authorization/token/registration 端点
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ServerMetadata 是 OAuth 授权服务器元数据中本流程关心的子集。
type ServerMetadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	RegistrationEndpoint  string `json:"registration_endpoint"`
}

// Discover 拉取 authorization-server metadata URL，返回端点集合。
func Discover(ctx context.Context, client *http.Client, metadataURL string) (*ServerMetadata, error) {
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, metadataURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build discovery request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("perform discovery request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("discovery failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var meta ServerMetadata
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return nil, fmt.Errorf("decode discovery response: %w", err)
	}
	if meta.AuthorizationEndpoint == "" || meta.TokenEndpoint == "" {
		return nil, fmt.Errorf("discovery response missing authorization_endpoint or token_endpoint")
	}
	return &meta, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run (sandbox disabled): `go test ./internal/oauth/ -run TestDiscover -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/oauth/discovery.go internal/oauth/discovery_test.go
git commit -m "feat(oauth): add single-hop authorization-server discovery"
```

---

## Task 3: Dynamic client registration

**Files:**
- Create: `internal/oauth/registration.go`
- Test: `internal/oauth/registration_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/oauth/registration_test.go`:

```go
package oauth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

	if _, err := RegisterClient(context.Background(), srv.Client(), srv.URL, ClientRegistrationRequest{}); err == nil {
		t.Error("expected error when client_id is missing")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./internal/oauth/ -run TestRegisterClient -v`
Expected: FAIL — `undefined: RegisterClient` / `undefined: ClientRegistrationRequest`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/oauth/registration.go`:

```go
/**
 * [INPUT]: 依赖 bytes、context、encoding/json、fmt、io、net/http、strings
 * [OUTPUT]: 对外提供 ClientRegistrationRequest / ClientRegistrationResponse 类型、RegisterClient 函数
 * [POS]: internal/oauth 的 RFC 7591 动态客户端注册，login 流程每次注册一个新 public client 拿 client_id
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ClientRegistrationRequest 是 RFC 7591 动态注册请求体。
type ClientRegistrationRequest struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method,omitempty"`
}

// ClientRegistrationResponse 仅取注册返回里本流程需要的 client_id。
type ClientRegistrationResponse struct {
	ClientID string `json:"client_id"`
}

// RegisterClient 向 registration_endpoint POST 动态注册一个客户端。
func RegisterClient(ctx context.Context, client *http.Client, endpoint string, request ClientRegistrationRequest) (*ClientRegistrationResponse, error) {
	if client == nil {
		client = http.DefaultClient
	}

	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("encode client registration request: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build client registration request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("perform client registration request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("client registration failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var payload ClientRegistrationResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode client registration response: %w", err)
	}
	if payload.ClientID == "" {
		return nil, fmt.Errorf("client registration response missing client_id")
	}
	return &payload, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run (sandbox disabled): `go test ./internal/oauth/ -run TestRegisterClient -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/oauth/registration.go internal/oauth/registration_test.go
git commit -m "feat(oauth): add RFC 7591 dynamic client registration"
```

---

## Task 4: Authorization URL + token exchange

**Files:**
- Create: `internal/oauth/flow.go`
- Test: `internal/oauth/flow_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/oauth/flow_test.go`:

```go
package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
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
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./internal/oauth/ -run 'TestBuildAuthorizationURL|TestExchange' -v`
Expected: FAIL — `undefined: BuildAuthorizationURL` / `undefined: ExchangeAuthorizationCode` / `undefined: AuthorizationRequest`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/oauth/flow.go`:

```go
/**
 * [INPUT]: 依赖 context、encoding/json、fmt、io、net/http、net/url、strings、time
 * [OUTPUT]: 对外提供 Token / AuthorizationRequest / TokenExchangeRequest 类型、BuildAuthorizationURL / ExchangeAuthorizationCode 函数（CallbackServer / OpenBrowser 在 Task 5 追加到本文件）
 * [POS]: internal/oauth 的登陆流程主原语——拼授权 URL、用 code 换 token；被 cmd/login.go 编排
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Token 是换取成功后的访问令牌。makecli 仅持久化 AccessToken，
// 其余字段用于成功提示（如 Expiry）。
type Token struct {
	AccessToken  string
	TokenType    string
	Scope        string
	RefreshToken string
	Expiry       time.Time
}

// AuthorizationRequest 是构建授权 URL 所需的参数集。
type AuthorizationRequest struct {
	AuthorizationEndpoint string
	BusinessType          string
	ClientID              string
	RedirectURL           string
	Resource              string
	Scopes                []string
	State                 string
	CodeChallenge         string
}

// TokenExchangeRequest 是 authorization_code 换 token 的参数集。
type TokenExchangeRequest struct {
	TokenEndpoint string
	ClientID      string
	Code          string
	CodeVerifier  string
	RedirectURL   string
	Resource      string
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
	RefreshToken string `json:"refresh_token"`
}

// BuildAuthorizationURL 把 AuthorizationRequest 拼成授权端点的完整 URL。
// Resource 为空时不带 resource 参数。
func BuildAuthorizationURL(request AuthorizationRequest) (string, error) {
	parsed, err := url.Parse(request.AuthorizationEndpoint)
	if err != nil {
		return "", fmt.Errorf("parse authorization endpoint: %w", err)
	}

	query := parsed.Query()
	query.Set("business_type", request.BusinessType)
	query.Set("client_id", request.ClientID)
	query.Set("code_challenge", request.CodeChallenge)
	query.Set("code_challenge_method", "S256")
	query.Set("redirect_uri", request.RedirectURL)
	if strings.TrimSpace(request.Resource) != "" {
		query.Set("resource", request.Resource)
	}
	query.Set("response_type", "code")
	query.Set("scope", strings.Join(request.Scopes, " "))
	query.Set("state", request.State)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

// ExchangeAuthorizationCode 用授权码换 token。
func ExchangeAuthorizationCode(ctx context.Context, client *http.Client, request TokenExchangeRequest) (*Token, error) {
	if client == nil {
		client = http.DefaultClient
	}

	form := url.Values{}
	form.Set("client_id", request.ClientID)
	form.Set("code", request.Code)
	form.Set("code_verifier", request.CodeVerifier)
	form.Set("grant_type", "authorization_code")
	form.Set("redirect_uri", request.RedirectURL)
	if strings.TrimSpace(request.Resource) != "" {
		form.Set("resource", request.Resource)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, request.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build token request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("perform token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
		return nil, fmt.Errorf("token request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var payload tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode token response: %w", err)
	}
	if payload.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token")
	}

	token := &Token{
		AccessToken:  payload.AccessToken,
		TokenType:    payload.TokenType,
		Scope:        payload.Scope,
		RefreshToken: payload.RefreshToken,
	}
	if payload.ExpiresIn > 0 {
		token.Expiry = time.Now().Add(time.Duration(payload.ExpiresIn) * time.Second)
	}
	return token, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run (sandbox disabled): `go test ./internal/oauth/ -run 'TestBuildAuthorizationURL|TestExchange' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/oauth/flow.go internal/oauth/flow_test.go
git commit -m "feat(oauth): add authorization-URL builder and token exchange"
```

---

## Task 5: Loopback callback server (ephemeral port) + browser launcher

**Files:**
- Modify: `internal/oauth/flow.go` (append callback server + OpenBrowser; update L3 header)
- Modify: `internal/oauth/flow_test.go` (append callback tests)

- [ ] **Step 1: Write the failing test**

Append to `internal/oauth/flow_test.go` (add imports `net`, `strings`, `time` if not present — final import block: `context`, `net`, `net/http`, `net/http/httptest`, `net/url`, `strings`, `testing`, `time`):

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./internal/oauth/ -run TestCallbackServer -v`
Expected: FAIL — `undefined: StartCallbackServer`.

- [ ] **Step 3: Write minimal implementation**

Append to `internal/oauth/flow.go` (and update the imports at the top to add `net`, `os/exec`, `runtime`, `sync` — final import block: `context`, `encoding/json`, `fmt`, `io`, `net`, `net/http`, `net/url`, `os/exec`, `runtime`, `strings`, `sync`, `time`). Also update the `[OUTPUT]` line of the L3 header to: `对外提供 Token / AuthorizationRequest / TokenExchangeRequest / CallbackServer 类型、BuildAuthorizationURL / ExchangeAuthorizationCode / StartCallbackServer / OpenBrowser 函数`):

```go
// ---------------------------------- 回调服务器 ----------------------------------

// CallbackServer 是登陆时临时起的本地 HTTP 服务，接收授权码回调。
// 绑定 127.0.0.1:0（OS 分配空闲端口），消除固定端口被占的特殊情况。
type CallbackServer struct {
	listener net.Listener
	server   *http.Server
	results  chan callbackResult
	once     sync.Once
}

type callbackResult struct {
	code  string
	state string
	err   string
}

// StartCallbackServer 在 127.0.0.1 上绑定一个空闲端口并开始监听 /callback，
// 返回服务器句柄与实际 redirectURL（含动态端口）。
func StartCallbackServer() (*CallbackServer, string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, "", fmt.Errorf("listen on loopback: %w", err)
	}
	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		_ = listener.Close()
		return nil, "", fmt.Errorf("unexpected listener address type %T", listener.Addr())
	}
	redirectURL := fmt.Sprintf("http://127.0.0.1:%d/callback", addr.Port)

	callback := &CallbackServer{
		listener: listener,
		results:  make(chan callbackResult, 1),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		callback.results <- callbackResult{
			code:  query.Get("code"),
			state: query.Get("state"),
			err:   query.Get("error"),
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, "Authorization received. You can return to the terminal.\n")
	})

	callback.server = &http.Server{Handler: mux}
	go func() { _ = callback.server.Serve(listener) }()

	return callback, redirectURL, nil
}

// Wait 阻塞直到收到回调、ctx 超时或取消；校验 state 后返回授权码。
func (s *CallbackServer) Wait(ctx context.Context, expectedState string) (string, error) {
	defer s.Close()

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("authorization cancelled or timed out: %w", ctx.Err())
	case result := <-s.results:
		if result.err != "" {
			return "", fmt.Errorf("authorization failed: %s", result.err)
		}
		if result.state != expectedState {
			return "", fmt.Errorf("authorization state mismatch")
		}
		if result.code == "" {
			return "", fmt.Errorf("authorization callback missing code")
		}
		return result.code, nil
	}
}

// Close 幂等关闭回调服务器。
func (s *CallbackServer) Close() {
	s.once.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = s.server.Shutdown(ctx)
	})
}

// ---------------------------------- 浏览器 ----------------------------------

// OpenBrowser 用平台默认方式打开 URL。
func OpenBrowser(authURL string) error {
	var command string
	switch runtime.GOOS {
	case "darwin":
		command = "open"
	case "linux":
		command = "xdg-open"
	case "windows":
		command = "rundll32"
	default:
		return fmt.Errorf("unsupported platform for browser auto-open")
	}

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command(command, "url.dll,FileProtocolHandler", authURL)
	} else {
		cmd = exec.Command(command, authURL)
	}
	return cmd.Start()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run (sandbox disabled): `go test ./internal/oauth/ -v`
Expected: PASS (all oauth tests, including callback + timeout).

- [ ] **Step 5: Commit**

```bash
git add internal/oauth/flow.go internal/oauth/flow_test.go
git commit -m "feat(oauth): add ephemeral-port loopback callback server and browser launcher"
```

---

## Task 6: internal/oauth L2 doc (GEB)

**Files:**
- Create: `internal/oauth/CLAUDE.md`

- [ ] **Step 1: Write the L2 module map**

Create `internal/oauth/CLAUDE.md`:

```markdown
# internal/oauth/
> L2 | 父级: /CLAUDE.md

## 成员清单
pkce.go:            PKCE 原语，提供 NewCodeVerifier / NewState（32 字节随机 → base64 raw-url，reader 可注入做确定性测试）/ S256Challenge（sha256 → base64rawurl）
pkce_test.go:       覆盖 NewCodeVerifier（确定性种子 + 短 reader 错误）/ S256Challenge（RFC 7636 Appendix B 向量）
discovery.go:       单跳 OAuth 元数据发现，提供 ServerMetadata 类型与 Discover（GET authorization-server metadata URL → authorization/token/registration 端点；缺 authz/token 端点报错）
discovery_test.go:  覆盖 Discover 成功解析与 500 错误路径，用 httptest 隔离网络
registration.go:    RFC 7591 动态客户端注册，提供 ClientRegistrationRequest/Response 与 RegisterClient（POST registration_endpoint → client_id；缺 client_id 报错）；login 每次注册新 public client
registration_test.go: 覆盖 RegisterClient 请求体断言/成功解析/缺 client_id 错误，用 httptest 隔离网络
flow.go:            登陆流程主原语，提供 Token / AuthorizationRequest / TokenExchangeRequest / CallbackServer 类型与 BuildAuthorizationURL（Resource 空则省略）/ ExchangeAuthorizationCode（authorization_code 换 token，解析 expires_in 为 Expiry）/ StartCallbackServer（绑定 127.0.0.1:0 空闲端口，返回 server + 动态 redirectURL）/ Wait（校验 state 取 code，支持 ctx 超时）/ OpenBrowser（跨平台打开 URL）
flow_test.go:       覆盖 BuildAuthorizationURL query 断言 / ExchangeAuthorizationCode 表单与 Expiry / 回调服务器成功-state不匹配-超时三态，用 httptest + 真实 loopback server

## 设计要点
- 纯协议原语，无 cobra、无持久化、无日志依赖；错误一律经 error 上抛，由 cmd/login.go 编排消费
- 动态端口（StartCallbackServer 绑 127.0.0.1:0）+ 每次注册新 client：两者互锁，消除固定端口与 client_id 持久化两个特殊情况
- 从 deps/contract-cli/internal/oauth 移植，砍掉 bot 的 tenant_access_token、protected-resource 两跳发现、slog.Logger

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
```

- [ ] **Step 2: Verify package still builds**

Run (sandbox disabled): `go build ./internal/oauth/ && go test ./internal/oauth/`
Expected: build clean, tests PASS (doc-only change, no code impact).

- [ ] **Step 3: Commit**

```bash
git add internal/oauth/CLAUDE.md
git commit -m "docs(oauth): add L2 module map for internal/oauth"
```

---

## Task 7: login command + orchestration

**Files:**
- Create: `cmd/login.go`
- Test: `cmd/login_test.go`

- [ ] **Step 1: Write the failing test**

Create `cmd/login_test.go`:

```go
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

func setMetadataURL(t *testing.T, srv *httptest.Server) {
	t.Helper()
	old := authMetadataURL
	authMetadataURL = srv.URL + "/.well-known/oauth-authorization-server/make"
	t.Cleanup(func() { authMetadataURL = old })
}

func TestRunLoginSuccess(t *testing.T) {
	fakeToken := "eyJ0eXAiOiJKV1QifQ.eyJzdWIiOiJ0ZXN0In0.c2lnbmF0dXJl"
	srv := newOAuthServer(t, true, fakeToken)
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	setMetadataURL(t, srv)
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
	setMetadataURL(t, srv)
	stubBrowserInjectCallback(t, "auth-code-1")
	setProfile(t, "todo")

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
	setMetadataURL(t, srv)
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
	setMetadataURL(t, srv)

	err := runLogin(5*time.Second, false)
	if err == nil {
		t.Fatal("expected error when registration_endpoint is absent")
	}
	if !strings.Contains(err.Error(), "registration_endpoint") {
		t.Errorf("error = %v, want mention of registration_endpoint", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (sandbox disabled): `go test ./cmd/ -run TestRunLogin -v`
Expected: FAIL — `undefined: runLogin` / `undefined: openBrowserFunc` / `undefined: authMetadataURL`.

- [ ] **Step 3: Write minimal implementation**

Create `cmd/login.go`:

```go
/**
 * [INPUT]: 依赖 internal/oauth（Discover/RegisterClient/StartCallbackServer/PKCE/BuildAuthorizationURL/ExchangeAuthorizationCode/OpenBrowser）、internal/config（Load/Save/CredentialsPath）、context、fmt、net/http、os、time、github.com/spf13/cobra；从 root.go 读取全局 Profile
 * [OUTPUT]: 对外提供 newLoginCmd 函数
 * [POS]: cmd 模块的 login 顶级命令，编排浏览器 OAuth 登陆，把 access_token 写入 ~/.make/credentials[Profile]
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/qfeius/makecli/internal/config"
	"github.com/qfeius/makecli/internal/oauth"
	"github.com/spf13/cobra"
)

// ---------------------------------- make dev preset ----------------------------------

// authMetadataURL 为 var 而非 const，便于单测指向 httptest。
var authMetadataURL = "https://dev-myaccount.qtech.cn/.well-known/oauth-authorization-server/make"

const (
	authBusinessType = "make"
	authResource     = "" // 留空：授权/换 token 不带 resource 参数
	authClientName   = "makecli"
)

var authScopes = []string{"mcp:tools", "mcp:resources"}

// openBrowserFunc 为包级可打桩变量，单测替换以免真浏览器（参照 deploy.go gitPushFunc 模式）。
var openBrowserFunc = oauth.OpenBrowser

// ---------------------------------- 命令 ----------------------------------

func newLoginCmd() *cobra.Command {
	var timeout time.Duration
	var noOpenBrowser bool

	cmd := &cobra.Command{
		Use:          "login",
		Short:        "Log in via browser and save the access token to ~/.make/credentials",
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runLogin(timeout, noOpenBrowser)
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", 3*time.Minute, "authorization timeout")
	cmd.Flags().BoolVar(&noOpenBrowser, "no-open-browser", false, "print the authorization URL instead of opening a browser")
	return cmd
}

// runLogin 执行完整登陆流程：discover → 起回调 server → 注册 client → PKCE →
// 浏览器 → 等回调 → 换 token → 写 credentials。
func runLogin(timeout time.Duration, noOpenBrowser bool) error {
	ctx := context.Background()
	httpClient := &http.Client{Timeout: 30 * time.Second}

	meta, err := oauth.Discover(ctx, httpClient, authMetadataURL)
	if err != nil {
		return err
	}
	if meta.RegistrationEndpoint == "" {
		return fmt.Errorf("authorization server does not advertise registration_endpoint; a fixed client_id is required")
	}

	// 先起回调 server 拿动态端口，redirectURL 要带进注册与授权 URL。
	callback, redirectURL, err := oauth.StartCallbackServer()
	if err != nil {
		return err
	}
	defer callback.Close()

	registration, err := oauth.RegisterClient(ctx, httpClient, meta.RegistrationEndpoint, oauth.ClientRegistrationRequest{
		ClientName:    authClientName,
		RedirectURIs:  []string{redirectURL},
		GrantTypes:    []string{"authorization_code"},
		ResponseTypes: []string{"code"},
	})
	if err != nil {
		return err
	}

	verifier, err := oauth.NewCodeVerifier(nil)
	if err != nil {
		return err
	}
	state, err := oauth.NewState(nil)
	if err != nil {
		return err
	}

	authURL, err := oauth.BuildAuthorizationURL(oauth.AuthorizationRequest{
		AuthorizationEndpoint: meta.AuthorizationEndpoint,
		BusinessType:          authBusinessType,
		ClientID:              registration.ClientID,
		RedirectURL:           redirectURL,
		Resource:              authResource,
		Scopes:                authScopes,
		State:                 state,
		CodeChallenge:         oauth.S256Challenge(verifier),
	})
	if err != nil {
		return err
	}

	if noOpenBrowser {
		fmt.Printf("Open this URL and finish authorization:\n%s\n", authURL)
	} else if err := openBrowserFunc(authURL); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to open browser: %v\n", err)
		fmt.Printf("Open this URL manually:\n%s\n", authURL)
	}

	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	code, err := callback.Wait(waitCtx, state)
	if err != nil {
		return err
	}

	token, err := oauth.ExchangeAuthorizationCode(ctx, httpClient, oauth.TokenExchangeRequest{
		TokenEndpoint: meta.TokenEndpoint,
		ClientID:      registration.ClientID,
		Code:          code,
		CodeVerifier:  verifier,
		RedirectURL:   redirectURL,
		Resource:      authResource,
	})
	if err != nil {
		return err
	}

	creds, err := config.Load()
	if err != nil {
		return err
	}
	p := creds[Profile]
	p.AccessToken = token.AccessToken
	creds[Profile] = p
	if err := config.Save(creds); err != nil {
		return err
	}

	path, _ := config.CredentialsPath()
	fmt.Printf("\nLogin succeeded for profile [%s].\n", Profile)
	fmt.Printf("Access token saved to %s\n", path)
	if !token.Expiry.IsZero() {
		fmt.Printf("Access token expires at: %s\n", token.Expiry.Format(time.RFC3339))
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run (sandbox disabled): `go test ./cmd/ -run TestRunLogin -v`
Expected: PASS (all four subtests: success, profile routing, no-open-browser timeout, missing registration_endpoint).

- [ ] **Step 5: Commit**

```bash
git add cmd/login.go cmd/login_test.go
git commit -m "feat(login): add browser OAuth login command orchestration"
```

---

## Task 8: Wire login into root command

**Files:**
- Modify: `cmd/root.go` (add `rootCmd.AddCommand(newLoginCmd())`)

- [ ] **Step 1: Locate the command registration block**

Run: `grep -n "AddCommand" cmd/root.go`
Expected: a sequence of `rootCmd.AddCommand(newXxxCmd())` lines. Note where top-level commands are mounted (e.g., near `newConfigureCmd()`).

- [ ] **Step 2: Add the login command registration**

In `cmd/root.go`, add this line alongside the other top-level `AddCommand` calls (place it next to `newConfigureCmd()` since login is auth-adjacent):

```go
	rootCmd.AddCommand(newLoginCmd())
```

- [ ] **Step 3: Verify it builds and the command is reachable**

Run (sandbox disabled):
```bash
make build && ./bin/makecli login --help
```
Expected: build succeeds; help shows `login`, flags `--timeout` and `--no-open-browser`, and the inherited global `--profile`.

- [ ] **Step 4: Run full package tests**

Run (sandbox disabled): `go test ./cmd/ ./internal/oauth/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/root.go
git commit -m "feat(login): mount login command on root"
```

---

## Task 9: GEB doc sync + full verification

**Files:**
- Modify: `cmd/CLAUDE.md` (add `login.go` / `login_test.go` to member list)
- Modify: `CLAUDE.md` (repo root: add `internal/oauth/` to `<directory>`, add `login` to cmd list)

- [ ] **Step 1: Update cmd/CLAUDE.md member list**

In `cmd/CLAUDE.md`, under `## 成员清单`, add two entries (place near `configure.go`):

```markdown
login.go:            login 顶级命令，浏览器 OAuth 登陆（复用全局 --profile，本地 --timeout 默认3m / --no-open-browser）；runLogin 编排 discover→起动态端口回调 server→每次新注册 client（RFC 7591）→PKCE→开浏览器→等回调→换 token，仅把 access_token 写入 ~/.make/credentials[Profile]；硬编码 make dev preset（business_type=make / scopes=mcp:tools mcp:resources / authMetadataURL 为 var 便于测试）；openBrowserFunc 包级可打桩变量
login_test.go:       覆盖 runLogin 的单元测试（成功写 token / 写入选定 profile / --no-open-browser 打印 URL 并超时 / 缺 registration_endpoint 报错），用 httptest 模拟 OAuth 三端点 + openBrowserFunc 桩注入回调，t.Setenv 隔离凭证
```

- [ ] **Step 2: Update root CLAUDE.md**

In the repo-root `CLAUDE.md` `<directory>` block, add a line for the new package:

```
internal/oauth/ - 浏览器 OAuth 登陆原语（PKCE + 单跳 discovery + RFC 7591 动态注册 + 授权URL/换token + 动态端口回调 server），从 contract-cli 移植，被 cmd/login 编排
```

And in the `cmd/` line of `<directory>`, append `login` to the subcommand enumeration. Find:

```
cmd/            - Cobra 子命令层（root、version、configure[token/config/set/get/verify]、app[create/list/init/delete/deploy]、entity、relation、record、apply、diff、update、schema、integration[ocr]、preflight）
```

Replace the trailing `、preflight）` with `、preflight、login）`.

- [ ] **Step 3: Full verification gate (must be exit 0 before commit)**

Run each separately, sandbox disabled, and confirm exit 0:
```bash
make vet
make test
golangci-lint run ./...
```
Expected: all clean. If golangci-lint flags `gocritic`/`unused`, fix inline (common: prefer `%w` wrapping already used; ensure no unused imports in test files — the flow_test.go import block must list exactly what's used).

- [ ] **Step 4: Commit**

```bash
git add cmd/CLAUDE.md CLAUDE.md
git commit -m "docs(login): sync GEB L1/L2 docs for login command and oauth package"
```

- [ ] **Step 5: Final smoke (optional, manual)**

If a dev token endpoint is reachable, run `./bin/makecli login --no-open-browser` and confirm it prints a `dev-myaccount.qtech.cn/.../authorize?...` URL with `business_type=make` and `scope=mcp:tools mcp:resources`. (Full end-to-end requires the live IdP; the unit tests already cover the orchestration against mocks.)

---

## Self-Review

**1. Spec coverage:**
- §2 command surface (`login`, `--timeout`, `--no-open-browser`, global `--profile`, no `--as`) → Task 7 (`newLoginCmd`) + Task 8 (mount).
- §3 flow (discover → ephemeral-port callback → register fresh → PKCE → browser → wait → exchange → write) → Tasks 1-5 (primitives) + Task 7 (orchestration). Ordering "callback before register" enforced in Task 7 Step 3.
- §4 preset (business_type=make, scopes, metadata URL, resource empty, clientName) → Task 7 constants.
- §5 storage (only access_token to credentials) → Task 7 `runLogin` final block.
- §6 package layout (internal/oauth + cmd/login.go, drop bot/slog/two-hop discovery) → Tasks 1-7.
- §7 error handling (non-2xx wrapped, timeout, state mismatch, missing registration_endpoint, browser-open warning non-fatal) → discovery/registration/flow error paths + Task 7 registration_endpoint check + warning branch.
- §8 testing (per-file oauth tests + cmd login test with mocked endpoints + stubbed browser) → test steps in every task.
- §9 risks (R1 missing registration_endpoint fallback message) → Task 7 explicit error; R2/R3 documented, no code.
- §10 GEB docs → Task 6 (oauth L2) + Task 9 (cmd L2, root L1).

**2. Placeholder scan:** No TBD/TODO; every code step contains full file/function bodies; every test step contains complete assertions. ✓

**3. Type consistency:** `ServerMetadata` (Task 2) used in Task 7. `ClientRegistrationRequest`/`ClientRegistrationResponse.ClientID` (Task 3) used in Task 7. `Token.AccessToken`/`Token.Expiry`, `AuthorizationRequest`, `TokenExchangeRequest`, `StartCallbackServer() (*CallbackServer, string, error)`, `(*CallbackServer).Wait/Close` (Tasks 4-5) all used with matching signatures in Task 7. `authMetadataURL` (var) / `openBrowserFunc` referenced identically in `cmd/login.go` and `cmd/login_test.go`. `config.Load`/`config.Save`/`config.Credentials`/`config.Profile.AccessToken`/`config.CredentialsPath` match `internal/config/credentials.go`. Global `Profile` matches `cmd` package convention. ✓
