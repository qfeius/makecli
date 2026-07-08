/**
 * [INPUT]: 依赖 context、fmt、os、github.com/olekukonko/tablewriter、github.com/spf13/cobra、internal/skillsync、cmd/output 辅助
 * [OUTPUT]: 对外提供 newSkillsListCmd 函数；包内 runSkillsList 被 skills 命令组默认行为复用
 * [POS]: cmd/skills 的 list 子命令，合并本地 lockfile 与 GitHub 远端状态，输出列 NAME/STATUS/DESCRIPTION/UPDATED AT；支持 table|json；远端失败降级 unknown + stderr 警告，退出码恒 0
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/olekukonko/tablewriter"
	"github.com/qfeius/makecli/internal/skillsync"
	"github.com/spf13/cobra"
)

// listSkillsFunc 包装 skillsync.List，便于测试打桩避免读真实 lockfile / 触网。
var listSkillsFunc = skillsync.List

func newSkillsListCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List Make platform skills and their remote status",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillsList(cmd.Context(), output)
		},
	}
	cmd.Flags().StringVar(&output, "output", outputTable, "output format (table|json)")
	return cmd
}

func runSkillsList(ctx context.Context, output string) error {
	if err := validateOutputFormat(output); err != nil {
		return err
	}

	inv := listSkillsFunc(ctx)

	if inv.LockWarning != "" {
		_, _ = fmt.Fprintf(os.Stderr, "warning: %s\n", inv.LockWarning)
	}
	if inv.RemoteErr != nil {
		_, _ = fmt.Fprintf(os.Stderr, "warning: remote check failed: %v\n", inv.RemoteErr)
	}

	if output == outputJSON {
		return writeJSON(map[string]any{"data": inv.Skills})
	}

	if len(inv.Skills) == 0 {
		fmt.Println("No Make platform skills installed.")
		fmt.Println("Run 'makecli skills update' to install.")
		return nil
	}

	rows := make([][]string, len(inv.Skills))
	for i, s := range inv.Skills {
		rows[i] = []string{s.Name, s.Status, truncateLine(s.Description, 60), shortDate(s.UpdatedAt)}
	}

	table := tablewriter.NewTable(os.Stdout)
	table.Header("NAME", "STATUS", "DESCRIPTION", "UPDATED AT")
	_ = table.Bulk(rows)
	_ = table.Render()

	installed, outdated, available := summarizeSkills(inv.Skills)
	fmt.Printf("\n%d installed, %d outdated, %d available\n", installed, outdated, available)
	if outdated+available > 0 {
		fmt.Println("Run 'makecli skills update' to install/upgrade.")
	}
	return nil
}

// summarizeSkills 统计已安装 / 落后 / 远端可装数量。
func summarizeSkills(skills []skillsync.SkillInfo) (installed, outdated, available int) {
	for _, s := range skills {
		switch s.Status {
		case skillsync.StatusNotInstalled:
			available++
		case skillsync.StatusOutdated:
			installed++
			outdated++
		default: // up-to-date / removed upstream / unknown 都属已安装
			installed++
		}
	}
	return installed, outdated, available
}

// truncateLine 把描述截到 max 个 rune 加省略号——表格列宽护栏，JSON 输出保留全文。
func truncateLine(s string, max int) string {
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

// shortDate 把 ISO 时间戳裁到日期部分——表格展示用，JSON 输出保留全值。
func shortDate(iso string) string {
	if len(iso) > 10 {
		return iso[:10]
	}
	return iso
}
