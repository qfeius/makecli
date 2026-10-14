/**
 * [INPUT]: 依赖 cmd/client、cmd/agent 的 validAgentAppKey、cmd/app_list 的 celString、cmd/output、internal/api，fmt、os、github.com/olekukonko/tablewriter、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newAgentListCmd；包内 runAgentList
 * [POS]: cmd/agent 的配置列表子命令，--app 经 CEL 等值筛选限定实际应用，分页与 auto/table/json 输出沿用 app list
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package cmd

import (
	"fmt"
	"os"

	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

func newAgentListCmd() *cobra.Command {
	var appKey, output string
	var page, size int
	cmd := &cobra.Command{
		Use: "list", Short: "List agents in an app", Args: cobra.NoArgs, SilenceUsage: true,
		Example: `  makecli agent list --app expense_app
  makecli agent list --app expense_app --page 2 --size 20 --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentList(appKey, page, size, output)
		},
	}
	cmd.Flags().StringVar(&appKey, "app", "", "actual environment app key (required)")
	_ = cmd.MarkFlagRequired("app")
	cmd.Flags().IntVar(&page, "page", 1, "page number to fetch (starts from 1)")
	cmd.Flags().IntVar(&size, "size", 20, "number of agents per page (1–100)")
	addOutputFlag(cmd, &output)
	return cmd
}

func runAgentList(appKey string, page, size int, output string) error {
	output, err := resolveOutputFormat(output)
	if err != nil {
		return err
	}
	if err := validAgentAppKey(appKey); err != nil {
		return err
	}
	if page < 1 || size < 1 || size > 100 {
		return fmt.Errorf("--page must be at least 1 and --size must be between 1 and 100")
	}
	client, err := newClientFromProfile()
	if err != nil {
		return err
	}
	agents, total, err := client.ListAgents(page, size, "appKey == "+celString(appKey))
	if err != nil {
		return err
	}
	if output == outputJSON {
		return writeJSON(map[string]any{
			"data":       agents,
			"pagination": map[string]int{"count": len(agents), "page": page, "size": size, "total": total},
		})
	}
	if len(agents) == 0 {
		fmt.Printf("No agents found in app '%s'.\n", appKey)
		return nil
	}
	rows := make([][]string, len(agents))
	for i, agent := range agents {
		rows[i] = []string{agent.Key, agent.Name, agent.Description, agent.ModelKey, agent.Status, agent.Meta.CreatedAt}
	}
	table := tablewriter.NewTable(os.Stdout)
	table.Header("KEY", "NAME", "DESCRIPTION", "MODEL", "STATUS", "CREATED AT")
	if err := table.Bulk(rows); err != nil {
		return err
	}
	if err := table.Render(); err != nil {
		return err
	}
	fmt.Printf("\nShowing %d of %d agents\n", len(agents), total)
	return nil
}
