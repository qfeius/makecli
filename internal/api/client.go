/**
 * [INPUT]: 依赖 bytes、encoding/json、fmt、net/http、time
 * [OUTPUT]: 对外提供 Client 类型、Option / WithDebug / WithHeaders 功能选项、New 构造函数、App / Field / Entity / EntityProperties / RelationEnd / RelationProperties / Relation / Schema 类型、CreateApp / ListApps / DeleteApp / GetApp / CreateEntity / ListEntities / GetEntity / UpdateEntity / DeleteEntity / CreateRelation / UpdateRelation / ListRelations / GetRelation / DeleteRelation / GetSchema 方法
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
func (c *Client) CreateApp(name string, properties map[string]any) error {
	body := map[string]any{
		"name":       name,
		"type":       "Make.App",
		"meta":       map[string]any{"version": "1.0.0"},
		"properties": properties,
	}
	return c.post("MakeService.CreateResource", "/meta/v1/app", body)
}

// ListApps 调用 MakeService.ListResources 获取 org 下全部 App
// filter 为可选的服务端过滤条件，nil 时不发送 filter 字段
// 返回 App 列表和服务端 total 数量
func (c *Client) ListApps(page, size int, filter map[string]any) ([]App, int, error) {
	reqBody := map[string]any{
		"sort":       []map[string]any{{"field": "id", "order": "asc"}},
		"pagination": map[string]any{"page": page, "size": size},
	}
	if filter != nil {
		reqBody["filter"] = filter
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
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	Meta        map[string]any `json:"meta"`
	Properties  map[string]any `json:"properties"`
	Validations map[string]any `json:"validations,omitempty"`
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

// ---------------------------------- Relation 操作 ----------------------------------

// RelationEnd 描述关系的一端（实体 + 基数）
type RelationEnd struct {
	Entity      string `json:"entity"`
	Cardinality string `json:"cardinality"` // "one" | "many"
}

// RelationProperties 封装 Relation 的 from/to 两端
type RelationProperties struct {
	From RelationEnd `json:"from"`
	To   RelationEnd `json:"to"`
}

// Relation 代表 Meta Server 返回的单个 Relation 资源
type Relation struct {
	Name       string             `json:"name"`
	Type       string             `json:"type"`
	App        string             `json:"app"`
	Meta       map[string]any     `json:"meta"`
	Properties RelationProperties `json:"properties"`
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

// CreateRelation 调用 MakeService.CreateResource 在指定 App 下创建 Relation
func (c *Client) CreateRelation(name, app string, props RelationProperties) error {
	body := map[string]any{
		"name": name,
		"type": "Make.Relation",
		"app":  app,
		"meta": map[string]any{"version": "1.0.0"},
		"properties": map[string]any{
			"from": props.From,
			"to":   props.To,
		},
	}
	return c.post("MakeService.CreateResource", "/meta/v1/relation", body)
}

// UpdateRelation 调用 MakeService.UpdateResource 更新指定 Relation
func (c *Client) UpdateRelation(name, app string, props RelationProperties) error {
	body := map[string]any{
		"name": name,
		"type": "Make.Relation",
		"app":  app,
		"meta": map[string]any{"version": "1.0.0"},
		"properties": map[string]any{
			"from": props.From,
			"to":   props.To,
		},
	}
	return c.post("MakeService.UpdateResource", "/meta/v1/relation", body)
}

// ListRelations 调用 MakeService.ListResources 获取指定 App 下全部 Relation
// 返回 Relation 列表和服务端 total 数量
func (c *Client) ListRelations(app string, page, size int) ([]Relation, int, error) {
	reqBody := map[string]any{
		"app":        app,
		"sort":       []map[string]any{{"field": "id", "order": "asc"}},
		"pagination": map[string]any{"page": page, "size": size},
	}
	var result struct {
		Code    int        `json:"code"`
		Message string     `json:"msg"`
		Data    []Relation `json:"data"`
		Pagination struct {
			Total int `json:"total"`
		} `json:"pagination"`
	}
	if err := c.do("MakeService.ListResources", "/meta/v1/relation", reqBody, &result); err != nil {
		return nil, 0, err
	}
	if result.Code != 200 {
		return nil, 0, fmt.Errorf("API 错误 [%d]: %s", result.Code, result.Message)
	}
	return result.Data, result.Pagination.Total, nil
}

// GetRelation 调用 MakeService.GetResource 获取指定 Relation 的详细信息
func (c *Client) GetRelation(app, name string) (*Relation, error) {
	reqBody := map[string]any{"app": app, "name": name}
	var result struct {
		Code    int      `json:"code"`
		Message string   `json:"msg"`
		Data    Relation `json:"data"`
	}
	if err := c.do("MakeService.GetResource", "/meta/v1/relation", reqBody, &result); err != nil {
		return nil, err
	}
	if result.Code != 200 {
		return nil, fmt.Errorf("API 错误 [%d]: %s", result.Code, result.Message)
	}
	return &result.Data, nil
}

// DeleteRelation 调用 MakeService.DeleteResource 删除指定 Relation
func (c *Client) DeleteRelation(name, app string) error {
	body := map[string]any{
		"name": name,
		"type": "Make.Relation",
		"app":  app,
	}
	return c.post("MakeService.DeleteResource", "/meta/v1/relation", body)
}

// ---------------------------------- Schema 操作 ----------------------------------

// Schema 代表 App 的聚合视图（App + Entities + Relations）
type Schema struct {
	App       App        `json:"app"`
	Entities  []Entity   `json:"entities"`
	Relations []Relation `json:"relations"`
}

// GetSchema 调用 MakeService.GetResource 获取指定 App 的聚合 Schema
func (c *Client) GetSchema(app string) (*Schema, error) {
	reqBody := map[string]any{"app": app}
	var result struct {
		Code    int    `json:"code"`
		Message string `json:"msg"`
		Data    Schema `json:"data"`
	}
	if err := c.do("MakeService.GetResource", "/meta/v1/schema", reqBody, &result); err != nil {
		return nil, err
	}
	if result.Code != 200 {
		return nil, fmt.Errorf("API 错误 [%d]: %s", result.Code, result.Message)
	}
	return &result.Data, nil
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
