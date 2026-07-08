/**
 * [INPUT]: 依赖 github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newSkillsCmd 函数
 * [POS]: cmd 模块的 skills 命令组，挂载 list 子命令；默认 RunE = list（参考 version.go 的 gh 模式）
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import "github.com/spf13/cobra"

func newSkillsCmd() *cobra.Command {
	var output string
	cmd := &cobra.Command{
		Use:          "skills",
		Short:        "Manage Make platform skills",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillsList(cmd.Context(), output)
		},
	}
	cmd.Flags().StringVar(&output, "output", outputTable, "output format (table|json)")
	cmd.AddCommand(newSkillsListCmd())
	return cmd
}
