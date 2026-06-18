/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、cmd/app（validResourceKey）、internal/api（Field）、encoding/json、fmt、os、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newEntityCreateCmd 函数
 * [POS]: cmd/entity 的 create 子命令，位置参数为 Entity key，--name 为展示名，--json 加载 fields；校验 field key 格式后调用 Meta Server API 创建 Entity；--dry-run 经 api.WithDryRun 注入 X-Dry-Run 让远端校验不落库，成功打印 would-be 行
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

func newEntityCreateCmd() *cobra.Command {
	var jsonFile string
	var displayName string
	var dryRun bool

	cmd := &cobra.Command{
		Use:          "create <key>",
		Short:        "Create a new entity on Make",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			appKey, _ := cmd.Parent().Flags().GetString("app")
			return runEntityCreate(args[0], displayName, appKey, jsonFile, dryRun)
		},
	}

	cmd.Flags().StringVar(&displayName, "name", "", "entity display name (defaults to key)")
	cmd.Flags().StringVar(&jsonFile, "json", "", "path to JSON file containing fields array")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate creation on Make without persisting")
	return cmd
}

func runEntityCreate(key, displayName, appKey, jsonFile string, dryRun bool) error {
	if err := validResourceKey(key); err != nil {
		return err
	}

	client, err := newClientFromProfile(api.WithDryRun(dryRun))
	if err != nil {
		return err
	}

	fields, err := loadFields(jsonFile)
	if err != nil {
		return err
	}

	for _, f := range fields {
		if err := validResourceKey(f.Key); err != nil {
			return fmt.Errorf("field key 校验失败 (%q): %w", f.Key, err)
		}
	}

	displayName = defaultName(displayName, key)

	if err := client.CreateEntity(key, displayName, appKey, fields); err != nil {
		return err
	}

	if dryRun {
		fmt.Printf("Dry run: entity '%s' would be created successfully in app '%s' (no changes made)\n", key, appKey)
		return nil
	}

	fmt.Printf("Entity '%s' created successfully in app '%s'\n", key, appKey)
	return nil
}

// loadFields 读取 JSON 文件并解析为 []Field；文件路径为空则返回空列表
func loadFields(path string) ([]api.Field, error) {
	if path == "" {
		return []api.Field{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 fields 文件失败: %w", err)
	}
	var fields []api.Field
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, fmt.Errorf("fields 文件格式错误（需为 JSON 数组）: %w", err)
	}
	return fields, nil
}
