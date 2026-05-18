/**
 * [INPUT]: 依赖 bytes、encoding/json、fmt、net/http、time
 * [OUTPUT]: 对外提供 Client 类型、Option / WithDebug / WithHeaders 功能选项、New 构造函数、App / Field / Entity / EntityProperties / RelationEnd / RelationProperties / Relation / Schema 类型、CreateApp(key, name, properties) / ListApps(page, size, filter) / DeleteApp(key) / GetApp(key) / CreateEntity(key, name, appKey, fields) / ListEntities(appKey, page, size, filter) / GetEntity(appKey, key) / UpdateEntity / DeleteEntity / CreateRelation(key, name, appKey, props) / UpdateRelation / ListRelations(appKey, ...) / GetRelation(appKey, key) / DeleteRelation / GetSchema(appKey) 方法。资源以 Key 为唯一标识符（英数下划线），Name 为用户可见展示名（支持中文）
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
// Key 是唯一标识符（英数下划线，2-20，创建后不可改）
// Name 是用户可见的展示名（允许中文，必填）
type App struct {
	Key        string         `json:"key"`
	Name       string         `json:"name"`
	Type       string         `json:"type"`
	Meta       map[string]any `json:"meta"`
	Properties map[string]any `json:"properties"`
}

// ---------------------------------- App 操作 ----------------------------------

// CreateApp 调用 MakeService.CreateResource 在 Meta Server 创建 App
// key 是英文标识符（不可改），name 是用户可见展示名（必填，支持中文）
func (c *Client) CreateApp(key, name string, properties map[string]any) error {
	body := map[string]any{
		"key":        key,
		"name":       name,
		"type":       "Make.App",
		"meta":       map[string]any{"version": "1.0.0"},
		"properties": properties,
	}
	return c.post("MakeService.CreateResource", "/meta/v1/app", body)
}

// ListApps 调用 MakeService.ListResources 获取 org 下全部 App
// filter 为可选的服务端过滤条件（对象数组，数组元素间为 OR），nil 时不发送 filter 字段
// 返回 App 列表和服务端 total 数量
func (c *Client) ListApps(page, size int, filter []map[string]any) ([]App, int, error) {
	reqBody := map[string]any{
		"sort":       []map[string]any{{"fieldKey": "id", "order": "asc"}},
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

// DeleteApp 调用 MakeService.DeleteResource 删除指定 App（按 key 定位）
func (c *Client) DeleteApp(key string) error {
	body := map[string]any{
		"key":  key,
		"type": "Make.App",
	}
	return c.post("MakeService.DeleteResource", "/meta/v1/app", body)
}

// ---------------------------------- Entity 操作 ----------------------------------

// Field 代表 Entity 下的单个字段定义
// Key 是 Entity 范围内唯一的标识符（英数下划线，2-20）
// Name 是用户可见的展示名（允许中文，必填）
type Field struct {
	Key         string         `json:"key"`
	Name        string         `json:"name"`
	Type        string         `json:"type"`
	Meta        map[string]any `json:"meta"`
	Properties  map[string]any `json:"properties"`
	Validations map[string]any `json:"validations,omitempty"`
}

// Entity 代表 Meta Server 返回的单个 Entity 资源
// Key 是 App 范围内唯一的标识符；AppKey 引用所属 App 的 key
type Entity struct {
	Key        string           `json:"key"`
	Name       string           `json:"name"`
	Type       string           `json:"type"`
	AppKey     string           `json:"appKey"`
	Meta       map[string]any   `json:"meta"`
	Properties EntityProperties `json:"properties"`
}

// EntityProperties 封装 Entity 的 fields 列表
type EntityProperties struct {
	Fields []Field `json:"fields"`
}

// ---------------------------------- Relation 操作 ----------------------------------

// RelationEnd 描述关系的一端（实体 key + 基数）
type RelationEnd struct {
	EntityKey   string `json:"entityKey"`
	Cardinality string `json:"cardinality"` // "one" | "many"
}

// RelationProperties 封装 Relation 的 from/to 两端
type RelationProperties struct {
	From RelationEnd `json:"from"`
	To   RelationEnd `json:"to"`
}

// Relation 代表 Meta Server 返回的单个 Relation 资源
// Key 是 App 范围内唯一的标识符；AppKey 引用所属 App 的 key
type Relation struct {
	Key        string             `json:"key"`
	Name       string             `json:"name"`
	Type       string             `json:"type"`
	AppKey     string             `json:"appKey"`
	Meta       map[string]any     `json:"meta"`
	Properties RelationProperties `json:"properties"`
}

// CreateEntity 调用 MakeService.CreateResource 在指定 App 下创建 Entity
// key 是 Entity 标识符（英数下划线），name 是展示名（必填）；appKey 引用所属 App
func (c *Client) CreateEntity(key, name, appKey string, fields []Field) error {
	body := map[string]any{
		"key":    key,
		"name":   name,
		"type":   "Make.Entity",
		"appKey": appKey,
		"meta":   map[string]any{"version": "1.0.0"},
		"properties": map[string]any{
			"fields": fields,
		},
	}
	return c.post("MakeService.CreateResource", "/meta/v1/entity", body)
}

// ListEntities 调用 MakeService.ListResources 获取指定 App 下全部 Entity
// filter 为可选的服务端过滤条件（对象数组，数组元素间为 OR），nil 时不发送 filter 字段
// 返回 Entity 列表和服务端 total 数量
func (c *Client) ListEntities(appKey string, page, size int, filter []map[string]any) ([]Entity, int, error) {
	reqBody := map[string]any{
		"appKey":     appKey,
		"sort":       []map[string]any{{"fieldKey": "id", "order": "asc"}},
		"pagination": map[string]any{"page": page, "size": size},
	}
	if filter != nil {
		reqBody["filter"] = filter
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

// GetEntity 调用 MakeService.GetResource 获取指定 Entity 的详细信息（按 key 定位）
func (c *Client) GetEntity(appKey, key string) (*Entity, error) {
	reqBody := map[string]any{"appKey": appKey, "key": key}
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

// GetApp 调用 MakeService.GetResource 获取指定 App（按 key 定位）
func (c *Client) GetApp(key string) (*App, error) {
	reqBody := map[string]any{"key": key}
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

// UpdateEntity 调用 MakeService.UpdateResource 更新指定 Entity（按 key 定位）
func (c *Client) UpdateEntity(key, name, appKey string, fields []Field) error {
	body := map[string]any{
		"key":    key,
		"name":   name,
		"type":   "Make.Entity",
		"appKey": appKey,
		"meta":   map[string]any{"version": "1.0.0"},
		"properties": map[string]any{
			"fields": fields,
		},
	}
	return c.post("MakeService.UpdateResource", "/meta/v1/entity", body)
}

// DeleteEntity 调用 MakeService.DeleteResource 删除指定 Entity（按 key 定位）
func (c *Client) DeleteEntity(key, appKey string) error {
	body := map[string]any{
		"key":    key,
		"type":   "Make.Entity",
		"appKey": appKey,
	}
	return c.post("MakeService.DeleteResource", "/meta/v1/entity", body)
}

// CreateRelation 调用 MakeService.CreateResource 在指定 App 下创建 Relation
// key 是 Relation 标识符，name 是展示名（必填）；appKey 引用所属 App
func (c *Client) CreateRelation(key, name, appKey string, props RelationProperties) error {
	body := map[string]any{
		"key":    key,
		"name":   name,
		"type":   "Make.Relation",
		"appKey": appKey,
		"meta":   map[string]any{"version": "1.0.0"},
		"properties": map[string]any{
			"from": props.From,
			"to":   props.To,
		},
	}
	return c.post("MakeService.CreateResource", "/meta/v1/relation", body)
}

// UpdateRelation 调用 MakeService.UpdateResource 更新指定 Relation（按 key 定位）
func (c *Client) UpdateRelation(key, name, appKey string, props RelationProperties) error {
	body := map[string]any{
		"key":    key,
		"name":   name,
		"type":   "Make.Relation",
		"appKey": appKey,
		"meta":   map[string]any{"version": "1.0.0"},
		"properties": map[string]any{
			"from": props.From,
			"to":   props.To,
		},
	}
	return c.post("MakeService.UpdateResource", "/meta/v1/relation", body)
}

// ListRelations 调用 MakeService.ListResources 获取指定 App 下全部 Relation
// filter 为可选的服务端过滤条件（对象数组，数组元素间为 OR），nil 时不发送 filter 字段
// 返回 Relation 列表和服务端 total 数量
func (c *Client) ListRelations(appKey string, page, size int, filter []map[string]any) ([]Relation, int, error) {
	reqBody := map[string]any{
		"appKey":     appKey,
		"sort":       []map[string]any{{"fieldKey": "id", "order": "asc"}},
		"pagination": map[string]any{"page": page, "size": size},
	}
	if filter != nil {
		reqBody["filter"] = filter
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

// GetRelation 调用 MakeService.GetResource 获取指定 Relation 的详细信息（按 key 定位）
func (c *Client) GetRelation(appKey, key string) (*Relation, error) {
	reqBody := map[string]any{"appKey": appKey, "key": key}
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

// DeleteRelation 调用 MakeService.DeleteResource 删除指定 Relation（按 key 定位）
func (c *Client) DeleteRelation(key, appKey string) error {
	body := map[string]any{
		"key":    key,
		"type":   "Make.Relation",
		"appKey": appKey,
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

// GetSchema 调用 MakeService.GetResource 获取指定 App 的聚合 Schema（按 appKey 定位）
func (c *Client) GetSchema(appKey string) (*Schema, error) {
	reqBody := map[string]any{"appKey": appKey}
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
