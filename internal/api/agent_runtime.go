/**
 * [INPUT]: 依赖 client.go 的 Client.do / authFailedErr / writeStatusErr，encoding/json、fmt
 * [OUTPUT]: 对外提供 AgentRuntime 类型和 Client.ListAgentRuntimes 方法
 * [POS]: internal/api 的 Agent 运行服务目录只读层，POST /agent/v1/runtime，不按 App 限定目录
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package api

import (
	"encoding/json"
	"fmt"
)

// AgentRuntime 是可选运行服务；Online 只代表在线状态，不承诺容量或执行成功。
type AgentRuntime struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Scope string `json:"scope"`
	Meta  struct {
		Online bool `json:"online"`
	} `json:"meta"`
}

func (c *Client) ListAgentRuntimes(page, size int) ([]AgentRuntime, int, error) {
	body := map[string]any{"pagination": map[string]int{"page": page, "size": size}}
	var result struct {
		Code       int             `json:"code"`
		Message    string          `json:"msg"`
		Data       json.RawMessage `json:"data"`
		Pagination struct {
			Total int `json:"total"`
		} `json:"pagination"`
	}
	if err := c.do("MakeService.ListResources", "/agent/v1/runtime", body, &result); err != nil {
		return nil, 0, err
	}
	if result.Code == 401 {
		return nil, 0, authFailedErr(result.Code, result.Message)
	}
	if result.Code != 200 {
		return nil, 0, writeStatusErr(result.Code, result.Message, result.Data)
	}
	var runtimes []AgentRuntime
	if err := json.Unmarshal(result.Data, &runtimes); err != nil {
		return nil, 0, fmt.Errorf("无效的 Agent 运行服务列表响应: %w", err)
	}
	if runtimes == nil {
		runtimes = []AgentRuntime{}
	}
	return runtimes, result.Pagination.Total, nil
}
