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
