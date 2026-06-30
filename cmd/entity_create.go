/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、cmd/app（validResourceKey）、internal/api（Field/EntityProperties/UniqueConstraint）、encoding/json、fmt、os、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newEntityCreateCmd 函数
 * [POS]: cmd/entity 的 create 子命令，位置参数为 Entity key，--name 为展示名，--json 加载整个 properties（fields + uniqueConstraints，同形于 DSL 与 entity list -o json 的 data.properties）；校验 field key 格式 + 约束字段引用后调用 Meta Server API 创建 Entity；--dry-run 经 api.WithDryRun 注入 X-Dry-Run 让远端校验不落库，成功打印 would-be 行
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
	cmd.Flags().StringVar(&jsonFile, "json", "", "path to JSON file with entity properties (fields + uniqueConstraints)")
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

	props, err := loadEntityProperties(jsonFile)
	if err != nil {
		return err
	}

	for _, f := range props.Fields {
		if err := validResourceKey(f.Key); err != nil {
			return fmt.Errorf("field key 校验失败 (%q): %w", f.Key, err)
		}
	}
	if err := validateConstraintFieldRefs(props); err != nil {
		return err
	}

	displayName = defaultName(displayName, key)

	if err := client.CreateEntity(key, displayName, appKey, props); err != nil {
		return err
	}

	if dryRun {
		fmt.Printf("Dry run: entity '%s' would be created successfully in app '%s' (no changes made)\n", key, appKey)
		return nil
	}

	fmt.Printf("Entity '%s' created successfully in app '%s'\n", key, appKey)
	return nil
}

// loadEntityProperties 读取 JSON 文件并解析为 EntityProperties（fields + uniqueConstraints）。
// 文件路径为空则返回仅含空 fields 的 properties。形态同 DSL 的 properties 与 entity list -o json 的 data.properties，三者可往返。
func loadEntityProperties(path string) (api.EntityProperties, error) {
	if path == "" {
		return api.EntityProperties{Fields: []api.Field{}}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return api.EntityProperties{}, fmt.Errorf("读取 properties 文件失败: %w", err)
	}
	var props api.EntityProperties
	if err := json.Unmarshal(data, &props); err != nil {
		return api.EntityProperties{}, fmt.Errorf("properties 文件格式错误（需为含 fields/uniqueConstraints 的 JSON 对象）: %w", err)
	}
	if props.Fields == nil {
		props.Fields = []api.Field{}
	}
	return props, nil
}

// validateConstraintFieldRefs 校验每个唯一约束引用的字段确实在 properties.fields 内（本地快速反馈）。
// 类型白名单 / 配额上限（≤5 约束、≤3 字段）/ 存量重复一律交服务端裁决，避免与后端规则漂移。
func validateConstraintFieldRefs(props api.EntityProperties) error {
	if len(props.UniqueConstraints) == 0 {
		return nil
	}
	known := make(map[string]bool, len(props.Fields))
	for _, f := range props.Fields {
		known[f.Key] = true
	}
	for _, c := range props.UniqueConstraints {
		if len(c.Fields) == 0 {
			return fmt.Errorf("唯一约束 %q 至少需要一个字段", c.Name)
		}
		for _, fk := range c.Fields {
			if !known[fk] {
				return fmt.Errorf("唯一约束 %q 引用了未声明的字段 %q", c.Name, fk)
			}
		}
	}
	return nil
}
