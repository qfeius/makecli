/**
 * [INPUT]: 依赖 client.go 的 Client.do / authFailedErr / writeStatusErr，encoding/json、fmt
 * [OUTPUT]: 对外提供 AgentPluginDefinition 类型和 Client.ListAgentPlugins 方法
 * [POS]: internal/api 的 Agent 插件目录只读层，POST /agent/v1/plugin，不按 App 限定目录
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package api

import (
	"encoding/json"
	"fmt"
)

// AgentPluginDefinition 是插件目录项，与 AgentPlugin 的已绑定配置分开。
type AgentPluginDefinition struct {
	Key          string         `json:"key"`
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	Status       string         `json:"status"`
	Capabilities []string       `json:"capabilities"`
	ConfigSchema map[string]any `json:"configSchema"`
}

func (c *Client) ListAgentPlugins(page, size int) ([]AgentPluginDefinition, int, error) {
	body := map[string]any{"pagination": map[string]int{"page": page, "size": size}}
	var result struct {
		Code       int             `json:"code"`
		Message    string          `json:"msg"`
		Data       json.RawMessage `json:"data"`
		Pagination struct {
			Total int `json:"total"`
		} `json:"pagination"`
	}
	if err := c.do("MakeService.ListResources", "/agent/v1/plugin", body, &result); err != nil {
		return nil, 0, err
	}
	if result.Code == 401 {
		return nil, 0, authFailedErr(result.Code, result.Message)
	}
	if result.Code != 200 {
		return nil, 0, writeStatusErr(result.Code, result.Message, result.Data)
	}
	var plugins []AgentPluginDefinition
	if err := json.Unmarshal(result.Data, &plugins); err != nil {
		return nil, 0, fmt.Errorf("无效的 Agent 插件列表响应: %w", err)
	}
	if plugins == nil {
		plugins = []AgentPluginDefinition{}
	}
	return plugins, result.Pagination.Total, nil
}
