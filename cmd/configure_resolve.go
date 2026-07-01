/**
 * [INPUT]: 依赖 internal/config、cmd/output、cmd/client 的全局 Profile / Environment / MetaServerURL 与 apiGatewayPath
 * [OUTPUT]: 对外提供 newConfigureResolveCmd 函数和 runConfigureResolve 白盒入口，输出本地预览所需的最小 JSON 解析结果
 * [POS]: cmd/configure 的 resolve 子命令，不联网校验 token，只解析当前 profile / environment / override 后的本地预览后端 origin
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"
	"strings"

	"github.com/qfeius/makecli/internal/config"
	"github.com/spf13/cobra"
)

const resolveTargetLocalPreview = "local-preview"

type configureResolveResult struct {
	Profile       string `json:"profile"`
	Environment   string `json:"environment"`
	MakeAPIOrigin string `json:"make_api_origin"`
	TenantID      string `json:"tenant_id"`
	OperatorID    string `json:"operator_id"`
}

func newConfigureResolveCmd() *cobra.Command {
	var target string
	var output string

	cmd := &cobra.Command{
		Use:          "resolve",
		Short:        "Resolve local MakeCLI configuration for tooling",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := runConfigureResolve(target, output)
			return err
		},
	}

	cmd.Flags().StringVar(&target, "target", resolveTargetLocalPreview, "resolve target (local-preview)")
	cmd.Flags().StringVar(&output, "output", outputJSON, "output format (json)")
	return cmd
}

func runConfigureResolve(target, output string) (*configureResolveResult, error) {
	if target != resolveTargetLocalPreview {
		return nil, fmt.Errorf("unsupported resolve target %q, valid options: %s", target, resolveTargetLocalPreview)
	}
	if output != outputJSON {
		return nil, fmt.Errorf("unsupported output format %q, valid options: %s", output, outputJSON)
	}
	if err := config.ValidateProfileName(Profile); err != nil {
		return nil, err
	}

	envName, env, err := resolveEnvironmentForConfigureResolve()
	if err != nil {
		return nil, err
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, err
	}
	cp := cfg[Profile]
	result := configureResolveResult{
		Profile:       Profile,
		Environment:   envName,
		MakeAPIOrigin: normalizeMakeAPIOrigin(firstNonEmpty(MetaServerURL, cp.MetaServerURL, env.MetaServerURL)),
		TenantID:      cp.XTenantID,
		OperatorID:    cp.OperatorID,
	}
	if err := writeJSON(result); err != nil {
		return nil, err
	}
	return &result, nil
}

func resolveEnvironmentForConfigureResolve() (string, config.Environment, error) {
	name := Environment
	if name == "" {
		settings, err := config.LoadSettings()
		if err != nil {
			return "", config.Environment{}, err
		}
		name = settings.Environment
	}
	if name == "" {
		name = config.DefaultEnvironment
	}
	env, ok := config.LookupEnvironment(name)
	if !ok {
		return "", config.Environment{}, fmt.Errorf("unknown environment %q, valid: %s", name, strings.Join(config.EnvironmentNames(), ", "))
	}
	return name, env, nil
}

func normalizeMakeAPIOrigin(url string) string {
	origin := strings.TrimRight(strings.TrimSpace(url), "/")
	return strings.TrimSuffix(origin, apiGatewayPath)
}
