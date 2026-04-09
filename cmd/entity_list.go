/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、internal/api（Client）、fmt、os、github.com/olekukonko/tablewriter、github.com/spf13/cobra、cmd/output 辅助、cmd/app_list（parseFilter）
 * [OUTPUT]: 对外提供 newEntityListCmd 函数
 * [POS]: cmd/entity 的 list 子命令，无 arg 时分页列出 app 下全部 entity，有 arg 时显示指定 entity 详情，支持 --filter / table|json 输出
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"
	"os"

	"github.com/qfeius/makecli/internal/api"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newEntityListCmd() *cobra.Command {
	var profile string
	var page int
	var size int
	var output string
	var filter string

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
			return runEntityList(app, entityName, profile, page, size, output, filter)
		},
	}

	cmd.Flags().StringVar(&profile, "profile", "default", "credentials profile to use")
	cmd.Flags().IntVar(&page, "page", 1, "page number to fetch (starts from 1)")
	cmd.Flags().IntVar(&size, "size", 20, "number of entities per page")
	cmd.Flags().StringVar(&output, "output", outputTable, "output format (table|json)")
	cmd.Flags().StringVar(&filter, "filter", "", `filter expression, e.g. "name=任务" (comma = OR)`)
	return cmd
}

func runEntityList(app, entityName, profile string, page, size int, output, filterExpr string) error {
	if err := validateOutputFormat(output); err != nil {
		return err
	}
	if page < 1 {
		return fmt.Errorf("page must be greater than or equal to 1")
	}
	if size < 1 {
		return fmt.Errorf("size must be greater than or equal to 1")
	}

	filter, err := parseFilter(filterExpr)
	if err != nil {
		return err
	}

	client, err := newClientFromProfile(profile)
	if err != nil {
		return err
	}

	if entityName != "" {
		return showEntity(client, app, entityName, output)
	}
	return listEntities(client, app, page, size, output, filter)
}

func listEntities(client *api.Client, app string, page, size int, output string, filter []map[string]any) error {
	entities, total, err := client.ListEntities(app, page, size, filter)
	if err != nil {
		return err
	}

	if output == outputJSON {
		return writeJSON(map[string]any{
			"data": entities,
			"pagination": map[string]int{
				"count": len(entities),
				"page":  page,
				"size":  size,
				"total": total,
			},
		})
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
	_ = table.Bulk(rows)
	_ = table.Render()

	fmt.Printf("\nShowing %d of %d entities\n", len(entities), total)
	return nil
}

func showEntity(client *api.Client, app, name, output string) error {
	entity, err := client.GetEntity(app, name)
	if err != nil {
		return err
	}

	if output == outputJSON {
		return writeJSON(map[string]any{
			"data": entity,
		})
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
	_ = table.Bulk(rows)
	_ = table.Render()
	return nil
}
