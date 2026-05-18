/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、fmt、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newRelationDeleteCmd 函数
 * [POS]: cmd/relation 的 delete 子命令，按 key 调用 Meta Server API 删除指定 Relation
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRelationDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "delete <key>",
		Short:        "Delete a relation on Make",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			appKey, _ := cmd.Parent().Flags().GetString("app")
			return runRelationDelete(args[0], appKey)
		},
	}

	return cmd
}

func runRelationDelete(key, appKey string) error {
	client, err := newClientFromProfile()
	if err != nil {
		return err
	}

	if err := client.DeleteRelation(key, appKey); err != nil {
		return err
	}

	fmt.Printf("Relation '%s' deleted successfully from app '%s'\n", key, appKey)
	return nil
}
