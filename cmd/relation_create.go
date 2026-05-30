/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、cmd/app（validResourceKey）、internal/api（RelationProperties/RelationEnd）、encoding/json、fmt、os、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newRelationCreateCmd 函数
 * [POS]: cmd/relation 的 create 子命令，位置参数为 Relation key，--name 为展示名，--json 加载 from/to 配置（entityKey 引用），调用 Meta Server API 创建 Relation
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/qfeius/makecli/internal/api"
	"github.com/spf13/cobra"
)

func newRelationCreateCmd() *cobra.Command {
	var jsonFile string
	var displayName string

	cmd := &cobra.Command{
		Use:          "create <key>",
		Short:        "Create a new relation on Make",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			appKey, _ := cmd.Parent().Flags().GetString("app")
			return runRelationCreate(args[0], displayName, appKey, jsonFile)
		},
	}

	cmd.Flags().StringVar(&displayName, "name", "", "relation display name (defaults to key)")
	cmd.Flags().StringVar(&jsonFile, "json", "", "path to JSON file containing relation properties (required)")
	_ = cmd.MarkFlagRequired("json")
	return cmd
}

func runRelationCreate(key, displayName, appKey, jsonFile string) error {
	if err := validResourceKey(key); err != nil {
		return err
	}

	client, err := newClientFromProfile()
	if err != nil {
		return err
	}

	props, err := loadRelationProperties(jsonFile)
	if err != nil {
		return err
	}

	displayName = defaultName(displayName, key)

	if err := client.CreateRelation(key, displayName, appKey, props); err != nil {
		return err
	}

	fmt.Printf("Relation '%s' created successfully in app '%s'\n", key, appKey)
	return nil
}

// loadRelationProperties 读取 JSON 文件并解析为 RelationProperties
func loadRelationProperties(path string) (api.RelationProperties, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return api.RelationProperties{}, fmt.Errorf("读取 JSON 文件失败: %w", err)
	}
	var props api.RelationProperties
	if err := json.Unmarshal(data, &props); err != nil {
		return api.RelationProperties{}, fmt.Errorf("JSON 文件格式错误（需包含 from/to 对象）: %w", err)
	}
	return props, nil
}
