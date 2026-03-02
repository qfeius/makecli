/**
 * [INPUT]: 依赖 bytes、encoding/json、fmt、net/http、time
 * [OUTPUT]: 对外提供 Client 类型和 New 构造函数、CreateApp 方法
 * [POS]: internal/api 的核心，封装 Make Meta Service 的 HTTP 调用
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ---------------------------------- 客户端 ----------------------------------

// Client 封装 Make Meta Service 的 HTTP 调用
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
}

// New 创建新的 API 客户端，30s 超时
func New(baseURL, token string) *Client {
	return &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ---------------------------------- 响应格式 ----------------------------------

type apiResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ---------------------------------- App 操作 ----------------------------------

// CreateApp 调用 MakeService.CreateResource 在 Meta Server 创建 App
func (c *Client) CreateApp(name string) error {
	body := map[string]any{
		"name": name,
		"type": "Make.App",
		"meta": map[string]any{"version": "1.0.0"},
		"properties": map[string]any{
			"code": name,
		},
	}
	return c.post("MakeService.CreateResource", "/meta/v1/app", body)
}

// ---------------------------------- 核心请求 ----------------------------------

// post 向 Meta Service 发 POST 请求，header 注入 target 和 token
func (c *Client) post(target, path string, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-Make-Target", target)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	var result apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("无效的响应格式: %w", err)
	}
	if result.Code != 200 {
		return fmt.Errorf("API 错误 [%d]: %s", result.Code, result.Message)
	}
	return nil
}
