/**
 * [INPUT]: 依赖 variable.go 的 getVariable/listVariables/deleteVariable、client.go 的 Client、debug.go 的 debugSink，encoding/json、strings
 * [OUTPUT]: 对外提供 Client.GetSecret / ListSecrets / DeleteSecret；包内 secretClient 与 redactSecretError 隔离密钥 debug 正文及错误回显
 * [POS]: internal/api 的密钥保密边界，复用 Variable 协议并保留错误链，不修改真实请求或共享客户端
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package api

import (
	"encoding/json"
	"strings"
)

// GetSecret 显式携带 Make.Secret，防止以普通变量类型查询密钥。
func (c *Client) GetSecret(key, appKey, scope string) (*Variable, error) {
	return c.secretClient().getVariable(key, appKey, scope, "Make.Secret")
}

// ListSecrets 在服务端筛选类型，分页 total 与返回条目均仅包含密钥。
func (c *Client) ListSecrets(appKey, scope string, page, size int) ([]Variable, int, error) {
	return c.listVariables(appKey, scope, "Make.Secret", page, size)
}

// DeleteSecret 只提交密钥身份，服务端事务校验类型，debug 不转储正文。
func (c *Client) DeleteSecret(key, appKey, scope string) error {
	return c.secretClient().deleteVariable(key, appKey, scope, "Make.Secret")
}

// secretClient 在调用内隔离 debug 状态，保留路由、耗时和状态，隐藏双方正文。
func (c *Client) secretClient() *Client {
	clone := *c
	if c.debug != nil {
		sink := *c.debug
		sink.hideBody = true
		clone.debug = &sink
	}
	return &clone
}

type redactedSecretError struct {
	cause   error
	message string
}

func (e *redactedSecretError) Error() string { return e.message }
func (e *redactedSecretError) Unwrap() error { return e.cause }

// redactSecretError 防止服务端错误反射提交值；保留哨兵判定与原有诊断。
func redactSecretError(err error, value *string) error {
	if err == nil || value == nil || *value == "" {
		return err
	}
	encoded, _ := json.Marshal(*value)
	message := strings.ReplaceAll(err.Error(), string(encoded[1:len(encoded)-1]), "[REDACTED]")
	message = strings.ReplaceAll(message, *value, "[REDACTED]")
	return &redactedSecretError{cause: err, message: message}
}
