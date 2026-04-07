/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、internal/api（Client）、github.com/spf13/cobra、cmd/output 辅助
 * [OUTPUT]: 对外提供 newSchemaCmd 函数
 * [POS]: cmd 的顶级子命令，获取指定 App 的聚合 Schema（App + Entities + Relations）并以 JSON 输出
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"github.com/spf13/cobra"
)

func newSchemaCmd() *cobra.Command {
	var app string
	var profile string

	cmd := &cobra.Command{
		Use:          "schema",
		Short:        "Get aggregated schema for an app (app + entities + relations)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSchema(app, profile)
		},
	}

	cmd.Flags().StringVar(&app, "app", "", "app name (required)")
	_ = cmd.MarkFlagRequired("app")
	cmd.Flags().StringVar(&profile, "profile", "default", "credentials profile to use")
	return cmd
}

func runSchema(app, profile string) error {
	client, err := newClientFromProfile(profile)
	if err != nil {
		return err
	}

	schema, err := client.GetSchema(app)
	if err != nil {
		return err
	}

	return writeJSON(schema)
}
