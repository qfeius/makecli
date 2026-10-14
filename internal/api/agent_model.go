/**
 * [INPUT]: 依赖 client.go 的 Client.do / authFailedErr / writeStatusErr，encoding/json、fmt
 * [OUTPUT]: 对外提供 AgentModel 类型和 Client.ListAgentModels 方法
 * [POS]: internal/api 的 Agent 模型目录只读层，POST /agent/v1/model，不按 App 限定目录
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package api

import (
	"encoding/json"
	"fmt"
)

// AgentModel 是获权模型目录项，Key 用于 Agent.modelKey。
type AgentModel struct {
	Key             string `json:"key"`
	Name            string `json:"name"`
	Provider        string `json:"provider"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	IsMultiModal    bool   `json:"isMultiModal"`
}

func (c *Client) ListAgentModels(page, size int) ([]AgentModel, int, error) {
	body := map[string]any{"pagination": map[string]int{"page": page, "size": size}}
	var result struct {
		Code       int             `json:"code"`
		Message    string          `json:"msg"`
		Data       json.RawMessage `json:"data"`
		Pagination struct {
			Total int `json:"total"`
		} `json:"pagination"`
	}
	if err := c.do("MakeService.ListResources", "/agent/v1/model", body, &result); err != nil {
		return nil, 0, err
	}
	if result.Code == 401 {
		return nil, 0, authFailedErr(result.Code, result.Message)
	}
	if result.Code != 200 {
		return nil, 0, writeStatusErr(result.Code, result.Message, result.Data)
	}
	var models []AgentModel
	if err := json.Unmarshal(result.Data, &models); err != nil {
		return nil, 0, fmt.Errorf("无效的 Agent 模型列表响应: %w", err)
	}
	if models == nil {
		models = []AgentModel{}
	}
	return models, result.Pagination.Total, nil
}
