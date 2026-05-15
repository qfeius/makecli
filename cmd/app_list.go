/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、fmt、os、strings、github.com/olekukonko/tablewriter、github.com/spf13/cobra、cmd/output 辅助
 * [OUTPUT]: 对外提供 newAppListCmd 函数、parseFilter 解析 --filter 语法
 * [POS]: cmd/app 的 list 子命令，分页列出 org 下全部 App，输出列 KEY/NAME/VERSION/CREATED AT；支持 --filter / table|json 输出；filter 支持 name(contains 模糊) / key(等值) / description
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newAppListCmd() *cobra.Command {
	var page int
	var size int
	var output string
	var filter string

	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List all apps",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppList(page, size, output, filter)
		},
	}

	cmd.Flags().IntVar(&page, "page", 1, "page number to fetch (starts from 1)")
	cmd.Flags().IntVar(&size, "size", 20, "number of apps per page")
	cmd.Flags().StringVar(&output, "output", outputTable, "output format (table|json)")
	cmd.Flags().StringVar(&filter, "filter", "", `filter expression, e.g. "name=待办,key=todo" (comma = OR; key 等值匹配, name/description 模糊匹配)`)
	return cmd
}

// parseFilter 解析 "key=value,key2=value2" 格式的过滤表达式
// 逗号分隔的每组 key=value 构成 OR 关系（数组中独立对象）
// 不同字段使用不同的匹配语义：
//   - key: 等值匹配 (=)
//   - name, description: 模糊匹配 (contains)
func parseFilter(expr string) ([]map[string]any, error) {
	if expr == "" {
		return nil, nil
	}
	var filters []map[string]any
	for _, part := range strings.Split(expr, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 || kv[0] == "" || kv[1] == "" {
			return nil, fmt.Errorf("invalid filter expression %q, expected key=value", part)
		}
		field, val := strings.TrimSpace(kv[0]), strings.TrimSpace(kv[1])
		switch field {
		case "key":
			filters = append(filters, map[string]any{field: map[string]any{"=": val}})
		case "name", "description":
			filters = append(filters, map[string]any{field: map[string]any{"contains": val}})
		default:
			return nil, fmt.Errorf("unsupported filter field %q", field)
		}
	}
	return filters, nil
}

func runAppList(page, size int, output, filterExpr string) error {
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

	client, err := newClientFromProfile()
	if err != nil {
		return err
	}

	apps, total, err := client.ListApps(page, size, filter)
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
		version, _ := app.Meta["version"].(string)
		createdAt, _ := app.Meta["createdAt"].(string)
		rows[i] = []string{app.Key, app.Name, version, createdAt}
	}

	table := tablewriter.NewTable(os.Stdout)
	table.Header("KEY", "NAME", "VERSION", "CREATED AT")
	_ = table.Bulk(rows)
	_ = table.Render()

	fmt.Printf("\nShowing %d of %d apps\n", len(apps), total)
	return nil
}
