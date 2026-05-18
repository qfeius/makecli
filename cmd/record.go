/**
 * [INPUT]: 依赖 github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newRecordCmd 函数
 * [POS]: cmd 模块的 record 命令组，挂载 create / get / update / delete / list 子命令，--app（appKey）和 --entity（entityKey）参数为子命令继承
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import "github.com/spf13/cobra"

func newRecordCmd() *cobra.Command {
	var appKey string
	var entityKey string

	cmd := &cobra.Command{
		Use:   "record",
		Short: "Manage records in an entity",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if appKey == "" || entityKey == "" {
				return cmd.Usage()
			}
			return nil
		},
	}

	cmd.PersistentFlags().StringVar(&appKey, "app", "", "app key (required)")
	_ = cmd.MarkPersistentFlagRequired("app")
	cmd.PersistentFlags().StringVar(&entityKey, "entity", "", "entity key (required)")
	_ = cmd.MarkPersistentFlagRequired("entity")

	cmd.AddCommand(newRecordCreateCmd())
	cmd.AddCommand(newRecordGetCmd())
	cmd.AddCommand(newRecordUpdateCmd())
	cmd.AddCommand(newRecordDeleteCmd())
	cmd.AddCommand(newRecordListCmd())
	return cmd
}
