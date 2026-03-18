/**
 * [INPUT]: 依赖 bytes、encoding/json、fmt、net/http、time
 * [OUTPUT]: 对外提供 Client 类型、Option / WithDebug / WithHeaders 功能选项、New 构造函数、App / Field / Entity / EntityProperties 类型、CreateApp / CreateAppWithCode / ListApps / DeleteApp / GetApp / CreateEntity / ListEntities / GetEntity / UpdateEntity / DeleteEntity 方法
 * [POS]: internal/api 的核心，封装 Make Meta Service 的 HTTP 调用
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// ---------------------------------- 客户端 ----------------------------------

// Client 封装 Make Meta Service 的 HTTP 调用
type Client struct {
	baseURL    string
	token      string
	httpClient *http.Client
	debug      bool
	headers    map[string]string
}

// Option 是 Client 的功能选项
type Option func(*Client)

// WithDebug 启用 debug 模式，输出 curl 命令到 stderr
func WithDebug(on bool) Option {
	return func(c *Client) { c.debug = on }
}

// WithHeaders 设置额外请求头（如 X-Tenant-ID、X-Operator-ID）
func WithHeaders(h map[string]string) Option {
	return func(c *Client) { c.headers = h }
}

// New 创建新的 API 客户端，30s 超时
func New(baseURL, token string, opts ...Option) *Client {
	c := &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
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
// code 默认为 name
func (c *Client) CreateApp(name string) error {
	return c.CreateAppWithCode(name, name)
}

// CreateAppWithCode 调用 MakeService.CreateResource 创建指定 code 的 App
func (c *Client) CreateAppWithCode(name, code string) error {
	body := map[string]any{
		"name": name,
		"type": "Make.App",
		"meta":  map[string]any{"version": "1.0.0"},
		"properties": map[string]any{
			"code": code,
		},
	}
	return c.post("MakeService.CreateResource", "/meta/v1/app", body)
}

// ListApps 调用 MakeService.ListResources 获取 org 下全部 App
// 返回 App 列表和服务端 total 数量
func (c *Client) ListApps(page, size int) ([]App, int, error) {
	reqBody := map[string]any{
		"sort":       []map[string]any{{"field": "id", "order": "asc"}},
		"pagination": map[string]any{"page": page, "size": size},
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

// ---------------------------------- Entity 操作 ----------------------------------

// Field 代表 Entity 下的单个字段定义
type Field struct {
	Name       string         `json:"name"`
	Type       string         `json:"type"`
	Meta       map[string]any `json:"meta"`
	Properties map[string]any `json:"properties"`
}

// Entity 代表 Meta Server 返回的单个 Entity 资源
type Entity struct {
	Name       string           `json:"name"`
	Type       string           `json:"type"`
	App        string           `json:"app"`
	Meta       map[string]any   `json:"meta"`
	Properties EntityProperties `json:"properties"`
}

// EntityProperties 封装 Entity 的 fields 列表
type EntityProperties struct {
	Fields []Field `json:"fields"`
}

// CreateEntity 调用 MakeService.CreateResource 在指定 App 下创建 Entity
func (c *Client) CreateEntity(name, app string, fields []Field) error {
	body := map[string]any{
		"name": name,
		"type": "Make.Entity",
		"app":  app,
		"meta": map[string]any{"version": "1.0.0"},
		"properties": map[string]any{
			"fields": fields,
		},
	}
	return c.post("MakeService.CreateResource", "/meta/v1/entity", body)
}

// ListEntities 调用 MakeService.ListResources 获取指定 App 下全部 Entity
// 返回 Entity 列表和服务端 total 数量
func (c *Client) ListEntities(app string, page, size int) ([]Entity, int, error) {
	reqBody := map[string]any{
		"app":        app,
		"sort":       []map[string]any{{"field": "id", "order": "asc"}},
		"pagination": map[string]any{"page": page, "size": size},
	}
	var result struct {
		Code    int      `json:"code"`
		Message string   `json:"msg"`
		Data    []Entity `json:"data"`
		Pagination struct {
			Total int `json:"total"`
		} `json:"pagination"`
	}
	if err := c.do("MakeService.ListResources", "/meta/v1/entity", reqBody, &result); err != nil {
		return nil, 0, err
	}
	if result.Code != 200 {
		return nil, 0, fmt.Errorf("API 错误 [%d]: %s", result.Code, result.Message)
	}
	return result.Data, result.Pagination.Total, nil
}

// GetEntity 调用 MakeService.GetResource 获取指定 Entity 的详细信息
func (c *Client) GetEntity(app, name string) (*Entity, error) {
	reqBody := map[string]any{"app": app, "name": name}
	var result struct {
		Code    int    `json:"code"`
		Message string `json:"msg"`
		Data    Entity `json:"data"`
	}
	if err := c.do("MakeService.GetResource", "/meta/v1/entity", reqBody, &result); err != nil {
		return nil, err
	}
	if result.Code != 200 {
		return nil, fmt.Errorf("API 错误 [%d]: %s", result.Code, result.Message)
	}
	return &result.Data, nil
}

// GetApp 调用 MakeService.GetResource 获取指定 App
func (c *Client) GetApp(name string) (*App, error) {
	reqBody := map[string]any{"name": name}
	var result struct {
		Code    int    `json:"code"`
		Message string `json:"msg"`
		Data    App    `json:"data"`
	}
	if err := c.do("MakeService.GetResource", "/meta/v1/app", reqBody, &result); err != nil {
		return nil, err
	}
	if result.Code != 200 {
		return nil, fmt.Errorf("API 错误 [%d]: %s", result.Code, result.Message)
	}
	return &result.Data, nil
}

// UpdateEntity 调用 MakeService.UpdateResource 更新指定 Entity
func (c *Client) UpdateEntity(name, app string, fields []Field) error {
	body := map[string]any{
		"name": name,
		"type": "Make.Entity",
		"app":  app,
		"meta": map[string]any{"version": "1.0.0"},
		"properties": map[string]any{
			"fields": fields,
		},
	}
	return c.post("MakeService.UpdateResource", "/meta/v1/entity", body)
}

// DeleteEntity 调用 MakeService.DeleteResource 删除指定 Entity
func (c *Client) DeleteEntity(name, app string) error {
	body := map[string]any{
		"name": name,
		"type": "Make.Entity",
		"app":  app,
	}
	return c.post("MakeService.DeleteResource", "/meta/v1/entity", body)
}

// ---------------------------------- 核心请求 ----------------------------------

// do 执行 POST 请求并将响应体解码到 result
func (c *Client) do(target, path string, body, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("序列化请求失败: %w", err)
	}

	// debug 模式：输出 curl 命令
	if c.debug {
		fmt.Fprintf(os.Stderr, "\n=== DEBUG: HTTP Request ===\n")
		fmt.Fprintf(os.Stderr, "curl -X POST '%s%s' \\\n", c.baseURL, path)
		fmt.Fprintf(os.Stderr, "  -H 'Content-Type: application/json' \\\n")
		fmt.Fprintf(os.Stderr, "  -H 'Authorization: Bearer %s' \\\n", c.token)
		fmt.Fprintf(os.Stderr, "  -H 'X-Make-Target: %s' \\\n", target)
		for k, v := range c.headers {
			fmt.Fprintf(os.Stderr, "  -H '%s: %s' \\\n", k, v)
		}
		fmt.Fprintf(os.Stderr, "  -d '%s'\n", string(data))
		fmt.Fprintf(os.Stderr, "==========================\n\n")
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-Make-Target", target)
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

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
