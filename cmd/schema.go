/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、internal/api（Client）、github.com/spf13/cobra、cmd/output 辅助
 * [OUTPUT]: 对外提供 newSchemaCmd 函数
 * [POS]: cmd 的顶级子命令，按 appKey 获取 App 的聚合 Schema（App + Entities + Relations）并以 JSON 输出
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"github.com/spf13/cobra"
)

func newSchemaCmd() *cobra.Command {
	var appKey string

	cmd := &cobra.Command{
		Use:          "schema",
		Short:        "Get aggregated schema for an app (app + entities + relations)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSchema(appKey)
		},
	}

	cmd.Flags().StringVar(&appKey, "app", "", "app key (required)")
	_ = cmd.MarkFlagRequired("app")
	return cmd
}

func runSchema(appKey string) error {
	client, err := newClientFromProfile()
	if err != nil {
		return err
	}

	schema, err := client.GetSchema(appKey)
	if err != nil {
		return err
	}

	return writeJSON(schema)
}
