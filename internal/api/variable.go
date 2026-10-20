/**
 * [INPUT]: 依赖 client.go 的 Client.do / Client.post / checkGetResult / authFailedErr / metaVersion、secret.go 的密钥脱敏，encoding/json、fmt
 * [OUTPUT]: 对外提供 Variable / VariableProperties 类型及 Client.GetVariable / ListVariables / CreateVariable / UpdateVariable / DeleteVariable；包内 getVariable 按类型查询
 * [POS]: internal/api 的配置 CRUD-L，appRole 由服务端解析实际 AppKey；Get/List 保留真实身份并校验 key/scope，列表按 properties.scope 筛选
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package api

import (
	"encoding/json"
	"fmt"
)

const variablePath = "/secret/v1/variable"

// Variable 同时承载普通变量与密钥的公开投影，密钥响应不包含 value。
type Variable struct {
	Key        string             `json:"key"`
	Type       string             `json:"type"`
	AppKey     string             `json:"appKey"`
	Meta       map[string]any     `json:"meta,omitempty"`
	Properties VariableProperties `json:"properties"`
}

type VariableProperties struct {
	Scope       string  `json:"scope"`
	Value       *string `json:"value,omitempty"`
	Description string  `json:"description,omitempty"`
}

// GetVariable 显式声明资源类型；只将明确的 404 视为不存在，损坏响应不得触发创建。
func (c *Client) GetVariable(key, appKey, scope string) (*Variable, error) {
	return c.getVariable(key, appKey, scope, "Make.Variable")
}

func (c *Client) getVariable(key, appKey, scope, resourceType string) (*Variable, error) {
	body := map[string]any{
		"key": key, "type": resourceType, "appKey": appKey,
		"properties": map[string]string{"scope": scope},
	}
	var result struct {
		Code    int             `json:"code"`
		Message string          `json:"msg"`
		Data    json.RawMessage `json:"data"`
	}
	if err := c.do("MakeService.GetResource", variablePath, body, &result); err != nil {
		return nil, err
	}
	if result.Code == 401 {
		return nil, authFailedErr(result.Code, result.Message)
	}
	if err := checkGetResult(result.Code, result.Message, true); err != nil {
		return nil, err
	}
	var variable Variable
	if err := json.Unmarshal(result.Data, &variable); err != nil {
		return nil, fmt.Errorf("无效的变量响应: %w", err)
	}
	if variable.Key != key || !c.matchesVariableApp(appKey, variable.AppKey) || variable.Properties.Scope != scope {
		return nil, fmt.Errorf("变量响应与请求的 key/scope 不一致或 appKey 无效")
	}
	if variable.Type != "Make.Variable" && variable.Type != "Make.Secret" {
		return nil, fmt.Errorf("无效的变量类型 %q", variable.Type)
	}
	return &variable, nil
}

// CreateVariable 与 UpdateVariable 共用写请求形态；value 指针保留显式空字符串。
func (c *Client) CreateVariable(variable Variable) error {
	return c.writeVariable("MakeService.CreateResource", variable)
}

func (c *Client) UpdateVariable(variable Variable) error {
	return c.writeVariable("MakeService.UpdateResource", variable)
}

// ListVariables 按 API 文档的 properties.scope 限定列表；debug 保留请求，只清理响应中的密钥值。
func (c *Client) ListVariables(appKey, scope string, page, size int) ([]Variable, int, error) {
	return c.listVariables(appKey, scope, "", page, size)
}

func (c *Client) listVariables(appKey, scope, resourceType string, page, size int) ([]Variable, int, error) {
	filter := "properties.scope == '" + scope + "'"
	if resourceType != "" {
		filter += " && type == '" + resourceType + "'"
	}
	body := map[string]any{
		"appKey":     appKey,
		"filter":     expression(filter),
		"pagination": map[string]int{"page": page, "size": size},
	}
	var result struct {
		Code       int             `json:"code"`
		Message    string          `json:"msg"`
		Data       json.RawMessage `json:"data"`
		Pagination struct {
			Total int `json:"total"`
		} `json:"pagination"`
	}
	if err := c.variableListClient().do("MakeService.ListResources", variablePath, body, &result); err != nil {
		return nil, 0, err
	}
	if result.Code == 401 {
		return nil, 0, authFailedErr(result.Code, result.Message)
	}
	if result.Code != 200 {
		return nil, 0, writeStatusErr(result.Code, result.Message, result.Data)
	}
	variables := []Variable{}
	if err := json.Unmarshal(result.Data, &variables); err != nil {
		return nil, 0, fmt.Errorf("无效的变量列表响应: %w", err)
	}
	if len(variables) > 0 && !c.matchesVariableApp(appKey, variables[0].AppKey) {
		return nil, 0, fmt.Errorf("变量列表响应的 appKey 无效")
	}
	for i, variable := range variables {
		if variable.AppKey != variables[0].AppKey || variable.Properties.Scope != scope || variable.Key == "" {
			return nil, 0, fmt.Errorf("变量列表响应混入不同应用、scope 不一致或缺少 key")
		}
		if variable.Type != "Make.Variable" && variable.Type != "Make.Secret" {
			return nil, 0, fmt.Errorf("无效的变量类型 %q", variable.Type)
		}
		if resourceType != "" && variable.Type != resourceType {
			return nil, 0, fmt.Errorf("变量列表响应类型与请求的 %s 不一致", resourceType)
		}
		if variable.Type == "Make.Secret" {
			variables[i].Properties.Value = nil
		}
	}
	if variables == nil {
		variables = []Variable{}
	}
	return variables, result.Pagination.Total, nil
}

// matchesVariableApp 有角色时允许服务端返回配对应用的实际 key，不推断命名后缀。
func (c *Client) matchesVariableApp(requested, actual string) bool {
	return actual != "" && (c.appRole != "" || actual == requested)
}

func (c *Client) variableListClient() *Client {
	clone := *c
	if c.debug != nil {
		sink := *c.debug
		sink.redactVariables = true
		clone.debug = &sink
	}
	return &clone
}

// DeleteVariable 只发送普通变量的完整命名空间，不携带值或修改应用清单。
func (c *Client) DeleteVariable(key, appKey, scope string) error {
	return c.deleteVariable(key, appKey, scope, "Make.Variable")
}

func (c *Client) deleteVariable(key, appKey, scope, resourceType string) error {
	body := map[string]any{
		"key": key, "type": resourceType, "appKey": appKey,
		"properties": map[string]string{"scope": scope},
	}
	return c.post("MakeService.DeleteResource", variablePath, body)
}

func (c *Client) writeVariable(target string, variable Variable) error {
	body := map[string]any{
		"key": variable.Key, "type": variable.Type, "appKey": variable.AppKey,
		"meta": map[string]string{"version": metaVersion}, "properties": variable.Properties,
	}
	if variable.Type == "Make.Secret" {
		err := c.secretClient().post(target, variablePath, body)
		return redactSecretError(err, variable.Properties.Value)
	}
	return c.post(target, variablePath, body)
}
