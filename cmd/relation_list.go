/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、internal/api（Client）、fmt、os、github.com/olekukonko/tablewriter、github.com/spf13/cobra、cmd/output 辅助、cmd/app_list（parseFilter）
 * [OUTPUT]: 对外提供 newRelationListCmd 函数
 * [POS]: cmd/relation 的 list 子命令，无 arg 时分页列出 app 下全部 relation，有 arg 时显示指定 relation 详情，支持 --filter / table|json 输出
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

func newRelationListCmd() *cobra.Command {
	var profile string
	var page int
	var size int
	var output string
	var filter string

	cmd := &cobra.Command{
		Use:          "list [relation-name]",
		Short:        "List relations in an app, or show a specific relation",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, _ := cmd.Parent().Flags().GetString("app")
			relationName := ""
			if len(args) == 1 {
				relationName = args[0]
			}
			return runRelationList(app, relationName, profile, page, size, output, filter)
		},
	}

	cmd.Flags().StringVar(&profile, "profile", "default", "credentials profile to use")
	cmd.Flags().IntVar(&page, "page", 1, "page number to fetch (starts from 1)")
	cmd.Flags().IntVar(&size, "size", 20, "number of relations per page")
	cmd.Flags().StringVar(&output, "output", outputTable, "output format (table|json)")
	cmd.Flags().StringVar(&filter, "filter", "", `filter expression, e.g. "name=project" (comma = OR)`)
	return cmd
}

func runRelationList(app, relationName, profile string, page, size int, output, filterExpr string) error {
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

	if relationName != "" {
		return showRelation(client, app, relationName, output)
	}
	return listRelations(client, app, page, size, output, filter)
}

func listRelations(client *api.Client, app string, page, size int, output string, filter []map[string]any) error {
	relations, total, err := client.ListRelations(app, page, size, filter)
	if err != nil {
		return err
	}

	if output == outputJSON {
		return writeJSON(map[string]any{
			"data": relations,
			"pagination": map[string]int{
				"count": len(relations),
				"page":  page,
				"size":  size,
				"total": total,
			},
		})
	}

	if len(relations) == 0 {
		fmt.Printf("No relations found in app '%s'.\n", app)
		return nil
	}

	rows := make([][]string, len(relations))
	for i, r := range relations {
		version, _ := r.Meta["version"].(string)
		from := fmt.Sprintf("%s(%s)", r.Properties.From.Entity, r.Properties.From.Cardinality)
		to := fmt.Sprintf("%s(%s)", r.Properties.To.Entity, r.Properties.To.Cardinality)
		rows[i] = []string{r.Name, from, to, version}
	}

	table := tablewriter.NewTable(os.Stdout)
	table.Header("NAME", "FROM", "TO", "VERSION")
	_ = table.Bulk(rows)
	_ = table.Render()

	fmt.Printf("\nShowing %d of %d relations\n", len(relations), total)
	return nil
}

func showRelation(client *api.Client, app, name, output string) error {
	relation, err := client.GetRelation(app, name)
	if err != nil {
		return err
	}

	if output == outputJSON {
		return writeJSON(map[string]any{
			"data": relation,
		})
	}

	version, _ := relation.Meta["version"].(string)
	fmt.Printf("Name:         %s\n", relation.Name)
	fmt.Printf("App:          %s\n", relation.App)
	fmt.Printf("Version:      %s\n", version)
	fmt.Printf("\nFrom:\n")
	fmt.Printf("  Entity:      %s\n", relation.Properties.From.Entity)
	fmt.Printf("  Cardinality: %s\n", relation.Properties.From.Cardinality)
	fmt.Printf("\nTo:\n")
	fmt.Printf("  Entity:      %s\n", relation.Properties.To.Entity)
	fmt.Printf("  Cardinality: %s\n", relation.Properties.To.Cardinality)
	return nil
}
