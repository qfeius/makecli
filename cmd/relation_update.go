/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、cmd/relation_create（loadRelationProperties）、fmt、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newRelationUpdateCmd 函数
 * [POS]: cmd/relation 的 update 子命令，按 key 定位，--name 更新展示名，--json 加载 from/to（entityKey 引用）调用 Meta Server API 更新 Relation
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRelationUpdateCmd() *cobra.Command {
	var jsonFile string
	var displayName string

	cmd := &cobra.Command{
		Use:          "update <key>",
		Short:        "Update an existing relation on Make",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			appKey, _ := cmd.Parent().Flags().GetString("app")
			return runRelationUpdate(args[0], displayName, appKey, jsonFile)
		},
	}

	cmd.Flags().StringVar(&displayName, "name", "", "relation display name (defaults to key)")
	cmd.Flags().StringVar(&jsonFile, "json", "", "path to JSON file containing relation properties (required)")
	_ = cmd.MarkFlagRequired("json")
	return cmd
}

func runRelationUpdate(key, displayName, appKey, jsonFile string) error {
	client, err := newClientFromProfile()
	if err != nil {
		return err
	}

	props, err := loadRelationProperties(jsonFile)
	if err != nil {
		return err
	}

	displayName = defaultName(displayName, key)

	if err := client.UpdateRelation(key, displayName, appKey, props); err != nil {
		return err
	}

	fmt.Printf("Relation '%s' updated successfully in app '%s'\n", key, appKey)
	return nil
}
