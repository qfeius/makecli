/**
 * [INPUT]: 依赖 internal/config（Load/Credentials）、internal/api（Client/New）、fmt、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newAppCreateCmd 函数
 * [POS]: cmd/app 的 create 子命令，调用 Meta Server API 创建 App
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"

	"github.com/MakeHQ/makecli/internal/api"
	"github.com/MakeHQ/makecli/internal/config"
	"github.com/spf13/cobra"
)

const defaultMetaServer = "https://dev-make.qtech.cn/api/make"

func newAppCreateCmd() *cobra.Command {
	var profile string
	var server string

	cmd := &cobra.Command{
		Use:          "create <name>",
		Short:        "Create a new app on Make",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppCreate(args[0], profile, server)
		},
	}

	cmd.Flags().StringVar(&profile, "profile", "default", "credentials profile to use")
	cmd.Flags().StringVar(&server, "server", defaultMetaServer, "Meta Server base URL")
	return cmd
}

func runAppCreate(name, profile, server string) error {
	creds, err := config.Load()
	if err != nil {
		return fmt.Errorf("加载凭证失败: %w", err)
	}

	p, ok := creds[profile]
	if !ok || p.AccessToken == "" {
		return fmt.Errorf("profile '%s' 未配置，请先运行: makecli configure --profile %s", profile, profile)
	}

	if err := api.New(server, p.AccessToken).CreateApp(name); err != nil {
		return err
	}

	fmt.Printf("App '%s' created successfully\n", name)
	return nil
}
