/**
 * [INPUT]: 依赖 cmd/client、cmd/output、fmt、os、strconv、github.com/olekukonko/tablewriter、github.com/spf13/cobra
 * [OUTPUT]: 包内 newAgentRuntimeCmd / newAgentRuntimeListCmd / runAgentRuntimeList
 * [POS]: cmd/agent runtime 目录命令组，list 分页查询可用运行服务，显示 key/name/scope/online，不要求 --app
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

func newAgentRuntimeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "runtime", Short: "Browse agent runtimes", Args: cobra.NoArgs, SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newAgentRuntimeListCmd())
	return cmd
}

func newAgentRuntimeListCmd() *cobra.Command {
	var output string
	var page, size int
	cmd := &cobra.Command{
		Use: "list", Short: "List available agent runtimes", Args: cobra.NoArgs, SilenceUsage: true,
		Example: "  makecli agent runtime list\n  makecli agent runtime list --page 2 --size 20 --output json",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentRuntimeList(page, size, output)
		},
	}
	cmd.Flags().IntVar(&page, "page", 1, "page number to fetch (starts from 1)")
	cmd.Flags().IntVar(&size, "size", 20, "number of runtimes per page (1–100)")
	addOutputFlag(cmd, &output)
	return cmd
}

func runAgentRuntimeList(page, size int, output string) error {
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
	runtimes, total, err := client.ListAgentRuntimes(page, size)
	if err != nil {
		return err
	}
	if output == outputJSON {
		return writeJSON(map[string]any{
			"data": runtimes, "pagination": map[string]int{"count": len(runtimes), "page": page, "size": size, "total": total},
		})
	}
	if len(runtimes) == 0 {
		fmt.Println("No agent runtimes found.")
		return nil
	}
	rows := make([][]string, len(runtimes))
	for i, runtime := range runtimes {
		rows[i] = []string{runtime.Key, runtime.Name, runtime.Scope, strconv.FormatBool(runtime.Meta.Online)}
	}
	table := tablewriter.NewTable(os.Stdout)
	table.Header("KEY", "NAME", "SCOPE", "ONLINE")
	if err := table.Bulk(rows); err != nil {
		return err
	}
	if err := table.Render(); err != nil {
		return err
	}
	fmt.Printf("\nShowing %d of %d runtimes\n", len(runtimes), total)
	return nil
}
