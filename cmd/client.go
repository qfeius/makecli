/**
 * [INPUT]: 依赖 internal/config（Load/LoadConfig/LoadSettings/LookupEnvironment）、internal/api（New/Option/WithDebug/WithHeaders）、fmt、strings；从 root.go 读取全局 Profile / MetaServerURL / RepoServerURL / Environment / DebugMode
 * [OUTPUT]: 对外提供 newClientFromProfile（变参 ...api.Option）/ newRepoClientFromProfile / resolveEnvironment / envName 函数、withGateway helper、apiGatewayPath 常量
 * [POS]: cmd 模块的公共 helper，统一「全局命令行入参 → API 客户端」的构建逻辑——profile / server / env / debug 全部由 root PersistentFlag 注入，子命令零参数调用；
 *        newClientFromProfile 收 ...api.Option 变参，把每命令横切选项（如 WithDryRun）追加到基础选项之后，写命令按需注入；
 *        resolveProfile 收口凭证与配置解析，resolveEnvironment 收口环境 preset；URL 取值链：flag > profile config > 环境 preset，主机基址再经 withGateway 补网关前缀 /api/make
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
	if err := config.ValidateProfileName(Profile); err != nil {
		return "", config.ConfigProfile{}, nil, err
	}

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

// apiGatewayPath 是后端网关挂载点，Meta / Data / Integration / Repo 请求共用此前缀。
// 主机基址（preset / config / flag）只描述部署端点，网关前缀由代码统一补齐，
// 使配置可写成纯主机名（https://test-make.qtech.cn）而非完整 URL。
const apiGatewayPath = "/api/make"

// withGateway 把网关前缀补到解析出的主机基址后。
// 幂等：已含前缀的完整 URL 原样返回，消除「配置该不该带 /api/make」的特殊情况；
// 空串原样返回（交由 api 层报错），尾随斜杠先行裁掉避免双斜杠。
func withGateway(host string) string {
	host = strings.TrimRight(host, "/")
	if host == "" || strings.HasSuffix(host, apiGatewayPath) {
		return host
	}
	return host + apiGatewayPath
}

// envName 解析当前后端环境名：--env flag > [settings] environment > DefaultEnvironment。
// 与 resolveEnvironment 同一解析链，但 fail-safe：吞掉 LoadSettings 错误回退默认，
// 供纯展示场景（如鉴权失败引导回显环境）使用——展示不该因配置读取失败而无名可显。
func envName() string {
	if Environment != "" {
		return Environment
	}
	if s, err := config.LoadSettings(); err == nil && s.Environment != "" {
		return s.Environment
	}
	return config.DefaultEnvironment
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
// profile / server / env / debug 四个全局态都来自 rootCmd 的 PersistentFlag，子命令无需也不应再传 profile。
// extra 是可选的每命令横切选项（如 WithDryRun）：基础选项之后追加，由具体写命令按需注入。
func newClientFromProfile(extra ...api.Option) (*api.Client, error) {
	token, cp, headers, err := resolveProfile()
	if err != nil {
		return nil, err
	}
	env, err := resolveEnvironment()
	if err != nil {
		return nil, err
	}
	server := withGateway(firstNonEmpty(MetaServerURL, cp.MetaServerURL, env.MetaServerURL))
	opts := append([]api.Option{api.WithDebug(DebugMode), api.WithHeaders(headers)}, extra...)
	return api.New(server, token, opts...), nil
}

// newRepoClientFromProfile 构建指向代码仓库服务（make-repo）的 API 客户端。
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
	server := withGateway(firstNonEmpty(RepoServerURL, cp.RepoServerURL, env.RepoServerURL))
	return api.New(server, token, api.WithDebug(DebugMode), api.WithHeaders(headers)), token, nil
}
