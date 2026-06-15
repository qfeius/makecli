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
