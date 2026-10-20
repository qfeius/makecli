/**
 * [INPUT]: 依赖 client.go 的 Client.do / Client.post / checkGetResult / authFailedErr / metaVersion，encoding/json、fmt
 * [OUTPUT]: 对外提供 Variable / VariableProperties 类型及 Client.GetVariable / CreateVariable / UpdateVariable
 * [POS]: internal/api 的应用配置读写层，调用 /secret/v1/variable；Get 显式传 type=Make.Variable，appRole 由客户端选项注入，scope 始终显式传递
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
	Properties VariableProperties `json:"properties"`
}

type VariableProperties struct {
	Scope       string  `json:"scope"`
	Value       *string `json:"value,omitempty"`
	Description string  `json:"description,omitempty"`
}

// GetVariable 显式声明资源类型；只将明确的 404 视为不存在，损坏响应不得触发创建。
func (c *Client) GetVariable(key, appKey, scope string) (*Variable, error) {
	body := map[string]any{
		"key": key, "type": "Make.Variable", "appKey": appKey,
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
	if variable.Key != key || variable.AppKey != appKey || variable.Properties.Scope != scope {
		return nil, fmt.Errorf("变量响应与请求的 key/appKey/scope 不一致")
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

func (c *Client) writeVariable(target string, variable Variable) error {
	body := map[string]any{
		"key": variable.Key, "type": variable.Type, "appKey": variable.AppKey,
		"meta": map[string]string{"version": metaVersion}, "properties": variable.Properties,
	}
	return c.post(target, variablePath, body)
}
