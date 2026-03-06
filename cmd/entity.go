/**
 * [INPUT]: 依赖 github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newEntityCmd 函数
 * [POS]: cmd 模块的 entity 命令组，挂载 create / list 子命令
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import "github.com/spf13/cobra"

func newEntityCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "entity",
		Short: "Manage entities",
	}
	cmd.AddCommand(newEntityCreateCmd())
	cmd.AddCommand(newEntityListCmd())
	return cmd
}
