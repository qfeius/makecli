/**
 * [INPUT]: 依赖 client.go 的 Client.do / writeStatusErr / authFailedErr、record.go 的 expression，encoding/json、fmt
 * [OUTPUT]: 对外提供 CreateAgentRequest / CreateAgentPlugin / AgentSummary / Agent / AgentPlugin 类型和 Client.CreateAgent / ListAgents / DeleteAgent / GetAgent / SetAgentStatus 方法
 * [POS]: internal/api 的 Agent 配置创建、列表、详情、删除与启停层，调用 /agent/v1/agent，appKey 表达实际应用归属；与本地聊天 internal/agent 无依赖
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package api

import (
	"encoding/json"
	"fmt"
)

// CreateAgentRequest 是配置面原生请求，不带 Meta DSL 的 type/properties 包装。
type CreateAgentRequest struct {
	Key         string              `json:"key"`
	Name        string              `json:"name"`
	AppKey      string              `json:"appKey"`
	ModelKey    string              `json:"modelKey"`
	RuntimeKey  string              `json:"runtimeKey,omitempty"`
	Description string              `json:"description,omitempty"`
	Prompt      string              `json:"prompt,omitempty"`
	Status      string              `json:"status,omitempty"`
	Plugins     []CreateAgentPlugin `json:"plugins,omitempty"`
}

// GetAgent 先检查状态再解码配置；404 与空 key 收敛为 ErrNotFound。
func (c *Client) GetAgent(key, appKey string) (*Agent, error) {
	var result struct {
		Code    int             `json:"code"`
		Message string          `json:"msg"`
		Data    json.RawMessage `json:"data"`
	}
	if err := c.do("MakeService.GetResource", "/agent/v1/agent", map[string]string{"key": key, "appKey": appKey}, &result); err != nil {
		return nil, err
	}
	if result.Code == 401 {
		return nil, authFailedErr(result.Code, result.Message)
	}
	if err := checkGetResult(result.Code, result.Message, true); err != nil {
		return nil, err
	}
	var agent Agent
	if err := json.Unmarshal(result.Data, &agent); err != nil {
		return nil, fmt.Errorf("无效的 Agent 响应: %w", err)
	}
	if err := checkGetResult(result.Code, result.Message, agent.Key != ""); err != nil {
		return nil, err
	}
	return &agent, nil
}

// CreateAgentPlugin 只接受创建字段；enabled 用指针区分省略与显式 false。
type CreateAgentPlugin struct {
	PluginKey string         `json:"pluginKey"`
	Enabled   *bool          `json:"enabled,omitempty"`
	Config    map[string]any `json:"config"`
}

// AgentSummary 是列表的公开概要，不展开 prompt 和 plugins。
type AgentSummary struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	AppKey      string `json:"appKey"`
	RuntimeKey  string `json:"runtimeKey"`
	ModelKey    string `json:"modelKey"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status"`
	Meta        struct {
		CreatedAt string `json:"createdAt"`
		UpdatedAt string `json:"updatedAt"`
	} `json:"meta"`
}

// Agent 是服务端保存后的公开配置，密钥由服务端按 writeOnly 规则剔除。
type Agent struct {
	AgentSummary
	Prompt  string        `json:"prompt"`
	Plugins []AgentPlugin `json:"plugins"`
}

// AgentPlugin 是绑定的公开投影，与不接受绑定 id 的创建输入分开。
type AgentPlugin struct {
	ID              string         `json:"id"`
	PluginKey       string         `json:"pluginKey"`
	Enabled         bool           `json:"enabled"`
	Config          map[string]any `json:"config"`
	ChannelEndpoint *struct {
		ServerURL string `json:"serverURL"`
	} `json:"channelEndpoint,omitempty"`
}

// CreateAgent 先判业务状态，再解码成功数据，避免错误详情误解为 Agent。
func (c *Client) CreateAgent(req CreateAgentRequest) (*Agent, error) {
	var result struct {
		Code    int             `json:"code"`
		Message string          `json:"msg"`
		Data    json.RawMessage `json:"data"`
	}
	if err := c.do("MakeService.CreateResource", "/agent/v1/agent", req, &result); err != nil {
		return nil, err
	}
	if result.Code == 401 {
		return nil, authFailedErr(result.Code, result.Message)
	}
	if result.Code != 200 {
		return nil, writeStatusErr(result.Code, result.Message, result.Data)
	}
	var saved Agent
	if err := json.Unmarshal(result.Data, &saved); err != nil {
		return nil, fmt.Errorf("无效的 Agent 响应: %w", err)
	}
	if saved.Key == "" {
		return nil, fmt.Errorf("创建 Agent 响应缺少 key")
	}
	return &saved, nil
}

// ListAgents 使用 CEL 筛选限定应用范围，先判业务状态再解码概要数组。
func (c *Client) ListAgents(page, size int, filter string) ([]AgentSummary, int, error) {
	body := map[string]any{
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
	if err := c.do("MakeService.ListResources", "/agent/v1/agent", body, &result); err != nil {
		return nil, 0, err
	}
	if result.Code == 401 {
		return nil, 0, authFailedErr(result.Code, result.Message)
	}
	if result.Code != 200 {
		return nil, 0, writeStatusErr(result.Code, result.Message, result.Data)
	}
	var agents []AgentSummary
	if err := json.Unmarshal(result.Data, &agents); err != nil {
		return nil, 0, fmt.Errorf("无效的 Agent 列表响应: %w", err)
	}
	if agents == nil {
		agents = []AgentSummary{}
	}
	return agents, result.Pagination.Total, nil
}

// DeleteAgent 以实际应用和 key 定位，服务端软删除并保留历史。
func (c *Client) DeleteAgent(key, appKey string) error {
	var result struct {
		Code    int             `json:"code"`
		Message string          `json:"msg"`
		Data    json.RawMessage `json:"data"`
	}
	body := map[string]string{"key": key, "appKey": appKey}
	if err := c.do("MakeService.DeleteResource", "/agent/v1/agent", body, &result); err != nil {
		return err
	}
	if result.Code == 401 {
		return authFailedErr(result.Code, result.Message)
	}
	if result.Code != 200 {
		return writeStatusErr(result.Code, result.Message, result.Data)
	}
	return nil
}

// SetAgentStatus 只提交 status，不发送其他配置字段；返回保存后的完整 Agent。
func (c *Client) SetAgentStatus(key, appKey, status string) (*Agent, error) {
	var result struct {
		Code    int             `json:"code"`
		Message string          `json:"msg"`
		Data    json.RawMessage `json:"data"`
	}
	body := map[string]string{"key": key, "appKey": appKey, "status": status}
	if err := c.do("MakeService.UpdateResource", "/agent/v1/agent", body, &result); err != nil {
		return nil, err
	}
	if result.Code == 401 {
		return nil, authFailedErr(result.Code, result.Message)
	}
	if result.Code != 200 {
		return nil, writeStatusErr(result.Code, result.Message, result.Data)
	}
	var saved Agent
	if err := json.Unmarshal(result.Data, &saved); err != nil {
		return nil, fmt.Errorf("无效的 Agent 响应: %w", err)
	}
	if saved.Key != key || saved.AppKey != appKey || saved.Status != status {
		return nil, fmt.Errorf("更新 Agent 响应与请求的 key/appKey/status 不一致")
	}
	return &saved, nil
}
