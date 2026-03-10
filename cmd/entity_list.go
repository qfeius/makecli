/**
 * [INPUT]: 依赖 internal/config（Load）、internal/api（Client/ListEntities/GetEntity）、fmt、os、github.com/olekukonko/tablewriter、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newEntityListCmd 函数
 * [POS]: cmd/entity 的 list 子命令，无 arg 时分页列出 app 下全部 entity，有 arg 时显示指定 entity 详情（name + fields）
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

func newEntityListCmd() *cobra.Command {
	var profile string
	var server string
	var size int

	cmd := &cobra.Command{
		Use:          "list [entity-name]",
		Short:        "List entities in an app, or show a specific entity",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, _ := cmd.Parent().Flags().GetString("app")
			entityName := ""
			if len(args) == 1 {
				entityName = args[0]
			}
			return runEntityList(app, entityName, profile, server, size)
		},
	}

	cmd.Flags().StringVar(&profile, "profile", "default", "credentials profile to use")
	cmd.Flags().StringVar(&server, "server", defaultMetaServer, "Meta Server base URL")
	cmd.Flags().IntVar(&size, "size", 20, "number of entities per page")
	return cmd
}

func runEntityList(app, entityName, profile, server string, size int) error {
	creds, err := config.Load()
	if err != nil {
		return fmt.Errorf("加载凭证失败: %w", err)
	}

	p, ok := creds[profile]
	if !ok || p.AccessToken == "" {
		return fmt.Errorf("profile '%s' 未配置，请先运行: makecli configure --profile %s", profile, profile)
	}

	client := api.New(server, p.AccessToken)
	if entityName != "" {
		return showEntity(client, app, entityName)
	}
	return listEntities(client, app, size)
}

func listEntities(client *api.Client, app string, size int) error {
	entities, total, err := client.ListEntities(app, 0, size)
	if err != nil {
		return err
	}

	if len(entities) == 0 {
		fmt.Printf("No entities found in app '%s'.\n", app)
		return nil
	}

	rows := make([][]string, len(entities))
	for i, e := range entities {
		version, _ := e.Meta["version"].(string)
		rows[i] = []string{e.Name, version}
	}

	table := tablewriter.NewTable(os.Stdout)
	table.Header("NAME", "VERSION")
	table.Bulk(rows)
	table.Render()

	fmt.Printf("\nShowing %d of %d entities\n", len(entities), total)
	return nil
}

func showEntity(client *api.Client, app, name string) error {
	entity, err := client.GetEntity(app, name)
	if err != nil {
		return err
	}

	version, _ := entity.Meta["version"].(string)
	fmt.Printf("Name:    %s\n", entity.Name)
	fmt.Printf("App:     %s\n", entity.App)
	fmt.Printf("Version: %s\n", version)

	if len(entity.Properties.Fields) == 0 {
		fmt.Println("\nNo fields.")
		return nil
	}

	fmt.Println("\nFields:")
	rows := make([][]string, len(entity.Properties.Fields))
	for i, f := range entity.Properties.Fields {
		rows[i] = []string{f.Name, f.Type}
	}
	table := tablewriter.NewTable(os.Stdout)
	table.Header("NAME", "TYPE")
	table.Bulk(rows)
	table.Render()
	return nil
}
