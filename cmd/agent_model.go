/**
 * [INPUT]: 依赖 cmd/client、cmd/output、fmt、os、strconv、github.com/olekukonko/tablewriter、github.com/spf13/cobra
 * [OUTPUT]: 包内 newAgentModelCmd / newAgentModelListCmd / runAgentModelList
 * [POS]: cmd/agent model 目录命令组，list 分页查询可用模型，表格显示 key/name/provider/model/isMultiModal，JSON 保留完整目录字段，不要求 --app
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package cmd

import (
	"fmt"
	"os"
	"strconv"

	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newAgentModelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "model", Short: "Browse agent models", Args: cobra.NoArgs, SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newAgentModelListCmd())
	return cmd
}

func newAgentModelListCmd() *cobra.Command {
	var output string
	var page, size int
	cmd := &cobra.Command{
		Use: "list", Short: "List available agent models", Args: cobra.NoArgs, SilenceUsage: true,
		Example: "  makecli agent model list\n  makecli agent model list --page 2 --size 20 --output json",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentModelList(page, size, output)
		},
	}
	cmd.Flags().IntVar(&page, "page", 1, "page number to fetch (starts from 1)")
	cmd.Flags().IntVar(&size, "size", 20, "number of models per page (1–100)")
	addOutputFlag(cmd, &output)
	return cmd
}

func runAgentModelList(page, size int, output string) error {
	output, err := resolveOutputFormat(output)
	if err != nil {
		return err
	}
	if page < 1 || size < 1 || size > 100 {
		return fmt.Errorf("--page must be at least 1 and --size must be between 1 and 100")
	}
	client, err := newClientFromProfile()
	if err != nil {
		return err
	}
	models, total, err := client.ListAgentModels(page, size)
	if err != nil {
		return err
	}
	if output == outputJSON {
		return writeJSON(map[string]any{
			"data": models, "pagination": map[string]int{"count": len(models), "page": page, "size": size, "total": total},
		})
	}
	if len(models) == 0 {
		fmt.Println("No agent models found.")
		return nil
	}
	rows := make([][]string, len(models))
	for i, model := range models {
		rows[i] = []string{model.Key, model.Name, model.Provider, model.Model, strconv.FormatBool(model.IsMultiModal)}
	}
	table := tablewriter.NewTable(os.Stdout)
	table.Header("KEY", "NAME", "PROVIDER", "MODEL", "MULTIMODAL")
	if err := table.Bulk(rows); err != nil {
		return err
	}
	if err := table.Render(); err != nil {
		return err
	}
	fmt.Printf("\nShowing %d of %d models\n", len(models), total)
	return nil
}
