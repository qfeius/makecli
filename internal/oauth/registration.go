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
