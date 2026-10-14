/**
 * [INPUT]: 依赖 cmd/client、cmd/output、fmt、os、strings、github.com/olekukonko/tablewriter、github.com/spf13/cobra
 * [OUTPUT]: 包内 newAgentPluginCmd / newAgentPluginListCmd / runAgentPluginList
 * [POS]: cmd/agent plugin 目录命令组，list 分页查询可用插件，表格显示 key/name/description/status/capabilities，JSON 保留完整目录字段，不要求 --app
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newAgentPluginCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "plugin", Short: "Browse agent plugins", Args: cobra.NoArgs, SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newAgentPluginListCmd())
	return cmd
}

func newAgentPluginListCmd() *cobra.Command {
	var output string
	var page, size int
	cmd := &cobra.Command{
		Use: "list", Short: "List available agent plugins", Args: cobra.NoArgs, SilenceUsage: true,
		Example: "  makecli agent plugin list\n  makecli agent plugin list --page 2 --size 20 --output json",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentPluginList(page, size, output)
		},
	}
	cmd.Flags().IntVar(&page, "page", 1, "page number to fetch (starts from 1)")
	cmd.Flags().IntVar(&size, "size", 20, "number of plugins per page (1–100)")
	addOutputFlag(cmd, &output)
	return cmd
}

func runAgentPluginList(page, size int, output string) error {
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
	plugins, total, err := client.ListAgentPlugins(page, size)
	if err != nil {
		return err
	}
	if output == outputJSON {
		return writeJSON(map[string]any{
			"data": plugins, "pagination": map[string]int{"count": len(plugins), "page": page, "size": size, "total": total},
		})
	}
	if len(plugins) == 0 {
		fmt.Println("No agent plugins found.")
		return nil
	}
	rows := make([][]string, len(plugins))
	for i, plugin := range plugins {
		rows[i] = []string{plugin.Key, plugin.Name, plugin.Description, plugin.Status, strings.Join(plugin.Capabilities, ", ")}
	}
	table := tablewriter.NewTable(os.Stdout)
	table.Header("KEY", "NAME", "DESCRIPTION", "STATUS", "CAPABILITIES")
	if err := table.Bulk(rows); err != nil {
		return err
	}
	if err := table.Render(); err != nil {
		return err
	}
	fmt.Printf("\nShowing %d of %d plugins\n", len(plugins), total)
	return nil
}
