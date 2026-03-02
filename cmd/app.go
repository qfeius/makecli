/**
 * [INPUT]: 依赖 github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newAppCmd 函数
 * [POS]: cmd 模块的 app 命令组，挂载 create / list 等子命令
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import "github.com/spf13/cobra"

func newAppCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "app",
		Short: "Manage apps",
	}
	cmd.AddCommand(newAppCreateCmd())
	cmd.AddCommand(newAppListCmd())
	cmd.AddCommand(newAppInitCmd())
	return cmd
}
