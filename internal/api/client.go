/**
 * [INPUT]: 依赖 bytes、encoding/json、fmt、net/http、time
 * [OUTPUT]: 对外提供 Client 类型、New 构造函数、App 类型、CreateApp / ListApps / DeleteApp 方法
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

// ---------------------------------- 数据类型 ----------------------------------

// App 代表 Meta Server 返回的单个 App 资源
type App struct {
	Name       string         `json:"name"`
	Type       string         `json:"type"`
	Meta       map[string]any `json:"meta"`
	Properties map[string]any `json:"properties"`
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

// ListApps 调用 MakeService.ListResources 获取 org 下全部 App
// 返回 App 列表和服务端 total 数量
func (c *Client) ListApps(offset, size int) ([]App, int, error) {
	reqBody := map[string]any{
		"sort":       []map[string]any{{"field": "id", "order": "asc"}},
		"pagination": map[string]any{"offset": offset, "size": size},
	}

	var result struct {
		Code    int    `json:"code"`
		Message string `json:"msg"`
		Data    []App  `json:"data"`
		Pagination struct {
			Total int `json:"total"`
		} `json:"pagination"`
	}
	if err := c.do("MakeService.ListResources", "/meta/v1/app", reqBody, &result); err != nil {
		return nil, 0, err
	}
	if result.Code != 200 {
		return nil, 0, fmt.Errorf("API 错误 [%d]: %s", result.Code, result.Message)
	}
	return result.Data, result.Pagination.Total, nil
}

// DeleteApp 调用 MakeService.DeleteResource 删除指定 App
func (c *Client) DeleteApp(name string) error {
	body := map[string]any{
		"name": name,
		"type": "Make.App",
	}
	return c.post("MakeService.DeleteResource", "/meta/v1/app", body)
}

// ---------------------------------- 核心请求 ----------------------------------

// do 执行 POST 请求并将响应体解码到 result
func (c *Client) do(target, path string, body, result any) error {
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

	if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
		return fmt.Errorf("无效的响应格式: %w", err)
	}
	return nil
}

// post 是 do 的便捷包装，用于只需检查 code == 200 的写操作
func (c *Client) post(target, path string, body any) error {
	var result struct {
		Code    int    `json:"code"`
		Message string `json:"msg"`
	}
	if err := c.do(target, path, body, &result); err != nil {
		return err
	}
	if result.Code != 200 {
		return fmt.Errorf("API 错误 [%d]: %s", result.Code, result.Message)
	}
	return nil
}
