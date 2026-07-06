/**
 * [INPUT]: 依赖 fmt、net/http，复用 client.go 的 request 原语与 authFailedErr 构造点
 * [OUTPUT]: 对外提供 UserTenant / UserInfo 类型、GetUserInfo() 方法
 * [POS]: internal/api 的用户身份查询层，封装网关 GET /user/v1/info（Make 网关接口说明）
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package api

import (
	"fmt"
	"net/http"
)

// UserTenant 描述当前用户归属的租户
type UserTenant struct {
	ID         string `json:"id"`
	TenantName string `json:"tenantName"`
}

// UserInfo 代表当前 token 对应的用户身份。
// Valid 表示当前用户与租户的关系是否有效。
type UserInfo struct {
	ID     string     `json:"id"`
	Name   string     `json:"name"`
	Tenant UserTenant `json:"tenant"`
	Valid  bool       `json:"valid"`
}

// notLoggedInCode 是用户信息接口表示「未登录 / Org 无法确认当前用户」的业务码。
// 该接口用直白的 401 而非 Meta 系的 990300403，两者都收敛到 ErrAuthFailed 哨兵。
const notLoggedInCode = 401

// GetUserInfo 调用 MakeService.GetResource 获取当前 token 对应的用户信息（GET /user/v1/info）。
// 未登录（业务码 401）返回 ErrAuthFailed，供 cmd 层统一翻译成重新登录引导。
func (c *Client) GetUserInfo() (*UserInfo, error) {
	var result struct {
		Code    int      `json:"code"`
		Message string   `json:"msg"`
		Data    UserInfo `json:"data"`
	}
	if err := c.request(http.MethodGet, "MakeService.GetResource", "/user/v1/info", nil, &result); err != nil {
		return nil, err
	}
	if result.Code == notLoggedInCode {
		return nil, authFailedErr(result.Code, result.Message)
	}
	if result.Code != 200 {
		return nil, fmt.Errorf("API 错误 [%d]: %s", result.Code, result.Message)
	}
	return &result.Data, nil
}
