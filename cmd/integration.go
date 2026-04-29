/**
 * [INPUT]: 依赖 github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newIntegrationCmd 函数
 * [POS]: cmd 模块的 integration 命令组，挂载 ocr 子命令；预留扩展点供未来其它 integration（如 translate / asr / embed）
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import "github.com/spf13/cobra"

func newIntegrationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "integration",
		Short: "Call integration services",
	}

	cmd.AddCommand(newIntegrationOCRCmd())
	return cmd
}
