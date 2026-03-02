/**
 * [INPUT]: 依赖 internal/config（Load）、internal/api（Client/ListApps）、fmt、os、github.com/olekukonko/tablewriter、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newAppListCmd 函数
 * [POS]: cmd/app 的 list 子命令，分页列出 org 下全部 App，tablewriter 渲染
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"
	"os"

	"github.com/MakeHQ/makecli/internal/api"
	"github.com/MakeHQ/makecli/internal/config"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newAppListCmd() *cobra.Command {
	var profile string
	var server string
	var size int

	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List all apps",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppList(profile, server, size)
		},
	}

	cmd.Flags().StringVar(&profile, "profile", "default", "credentials profile to use")
	cmd.Flags().StringVar(&server, "server", defaultMetaServer, "Meta Server base URL")
	cmd.Flags().IntVar(&size, "size", 20, "number of apps per page")
	return cmd
}

func runAppList(profile, server string, size int) error {
	creds, err := config.Load()
	if err != nil {
		return fmt.Errorf("加载凭证失败: %w", err)
	}

	p, ok := creds[profile]
	if !ok || p.AccessToken == "" {
		return fmt.Errorf("profile '%s' 未配置，请先运行: makecli configure --profile %s", profile, profile)
	}

	apps, total, err := api.New(server, p.AccessToken).ListApps(0, size)
	if err != nil {
		return err
	}

	if len(apps) == 0 {
		fmt.Println("No apps found.")
		return nil
	}

	rows := make([][]string, len(apps))
	for i, app := range apps {
		code, _ := app.Properties["code"].(string)
		version, _ := app.Meta["version"].(string)
		rows[i] = []string{app.Name, code, version}
	}

	table := tablewriter.NewTable(os.Stdout)
	table.Header("NAME", "CODE", "VERSION")
	table.Bulk(rows)
	table.Render()

	fmt.Printf("\nShowing %d of %d apps\n", len(apps), total)
	return nil
}
