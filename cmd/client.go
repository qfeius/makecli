/**
 * [INPUT]: 依赖 internal/config（Load/LoadConfig/LoadSettings/LookupEnvironment）、internal/api（New/WithDebug/WithHeaders）、fmt、strings；从 root.go 读取全局 Profile / ServerURL / RepoServerURL / Environment / DebugMode
 * [OUTPUT]: 对外提供 newClientFromProfile / newRepoClientFromProfile / resolveEnvironment 函数
 * [POS]: cmd 模块的公共 helper，统一「全局命令行入参 → API 客户端」的构建逻辑——profile / server / env / debug 全部由 root PersistentFlag 注入，子命令零参数调用；
 *        resolveProfile 收口凭证与配置解析，resolveEnvironment 收口环境 preset；URL 取值链：flag > profile config > 环境 preset
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"
	"strings"

	"github.com/qfeius/makecli/internal/api"
	"github.com/qfeius/makecli/internal/config"
)

// resolveProfile 按当前全局 Profile 读取凭证与配置，返回 token、profile 配置与附加 headers
func resolveProfile() (string, config.ConfigProfile, map[string]string, error) {
	creds, err := config.Load()
	if err != nil {
		return "", config.ConfigProfile{}, nil, fmt.Errorf("加载凭证失败: %w", err)
	}

	p, ok := creds[Profile]
	if !ok || p.AccessToken == "" {
		return "", config.ConfigProfile{}, nil, fmt.Errorf("profile '%s' 未配置，请先运行: makecli configure --profile %s", Profile, Profile)
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		return "", config.ConfigProfile{}, nil, fmt.Errorf("加载配置失败: %w", err)
	}

	cp := cfg[Profile]
	headers := map[string]string{}
	if cp.XTenantID != "" {
		headers["X-Tenant-ID"] = cp.XTenantID
	}
	if cp.OperatorID != "" {
		headers["X-Operator-ID"] = cp.OperatorID
	}
	return p.AccessToken, cp, headers, nil
}

// firstNonEmpty 返回第一个非空字符串，统一「flag > config > 环境 preset」三级取值链
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// resolveEnvironment 解析当前后端环境 preset：--env flag > [settings] environment > DefaultEnvironment。
// 未知环境名（typo / 非法手抄）报错，避免静默落到错误后端。
func resolveEnvironment() (config.Environment, error) {
	name := Environment
	if name == "" {
		settings, err := config.LoadSettings()
		if err != nil {
			return config.Environment{}, err
		}
		name = settings.Environment
	}
	env, ok := config.LookupEnvironment(name)
	if !ok {
		return config.Environment{}, fmt.Errorf("unknown environment %q, valid: %s", name, strings.Join(config.EnvironmentNames(), ", "))
	}
	return env, nil
}

// newClientFromProfile 构建指向 Meta/Data Server 的 API 客户端。
// 三个全局态都来自 rootCmd 的 PersistentFlag，子命令无需也不应再传参。
func newClientFromProfile() (*api.Client, error) {
	token, cp, headers, err := resolveProfile()
	if err != nil {
		return nil, err
	}
	env, err := resolveEnvironment()
	if err != nil {
		return nil, err
	}
	server := firstNonEmpty(ServerURL, cp.ServerURL, env.MetaServerURL)
	return api.New(server, token, api.WithDebug(DebugMode), api.WithHeaders(headers)), nil
}

// newRepoClientFromProfile 构建指向代码仓库服务（make-gitea）的 API 客户端。
// 额外返回裸 token，供 deploy 的 git push HTTP Basic 认证使用。
func newRepoClientFromProfile() (*api.Client, string, error) {
	token, cp, headers, err := resolveProfile()
	if err != nil {
		return nil, "", err
	}
	env, err := resolveEnvironment()
	if err != nil {
		return nil, "", err
	}
	server := firstNonEmpty(RepoServerURL, cp.RepoServerURL, env.RepoServerURL)
	return api.New(server, token, api.WithDebug(DebugMode), api.WithHeaders(headers)), token, nil
}
