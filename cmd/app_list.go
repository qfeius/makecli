/**
 * [INPUT]: 依赖 internal/config（Load）、internal/api（Client/ListApps）、fmt、os、github.com/olekukonko/tablewriter、github.com/spf13/cobra、cmd/output 辅助
 * [OUTPUT]: 对外提供 newAppListCmd 函数
 * [POS]: cmd/app 的 list 子命令，分页列出 org 下全部 App，支持 table/json 输出
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
	var page int
	var size int
	var output string

	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List all apps",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppList(profile, server, page, size, output)
		},
	}

	cmd.Flags().StringVar(&profile, "profile", "default", "credentials profile to use")
	cmd.Flags().StringVar(&server, "server", defaultMetaServer, "Meta Server base URL")
	cmd.Flags().IntVar(&page, "page", 1, "page number to fetch (starts from 1)")
	cmd.Flags().IntVar(&size, "size", 20, "number of apps per page")
	cmd.Flags().StringVar(&output, "output", outputTable, "output format (table|json)")
	return cmd
}

func runAppList(profile, server string, page, size int, output string) error {
	if err := validateOutputFormat(output); err != nil {
		return err
	}
	if page < 1 {
		return fmt.Errorf("page must be greater than or equal to 1")
	}

	creds, err := config.Load()
	if err != nil {
		return fmt.Errorf("加载凭证失败: %w", err)
	}

	p, ok := creds[profile]
	if !ok || p.AccessToken == "" {
		return fmt.Errorf("profile '%s' 未配置，请先运行: makecli configure --profile %s", profile, profile)
	}

	apps, total, err := api.New(server, p.AccessToken, DebugMode).ListApps(page, size)
	if err != nil {
		return err
	}

	if output == outputJSON {
		return writeJSON(map[string]any{
			"data": apps,
			"pagination": map[string]int{
				"count": len(apps),
				"page":  page,
				"size":  size,
				"total": total,
			},
		})
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
