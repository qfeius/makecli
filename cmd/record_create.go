/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、encoding/json、fmt、os、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newRecordCreateCmd 函数、loadRecordData 辅助函数
 * [POS]: cmd/record 的 create 子命令，从 JSON 文件加载数据，调用 Data Service API 创建 Record
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func newRecordCreateCmd() *cobra.Command {
	var profile string
	var jsonFile string

	cmd := &cobra.Command{
		Use:          "create",
		Short:        "Create a new record in an entity",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, _ := cmd.Parent().Flags().GetString("app")
			entity, _ := cmd.Parent().Flags().GetString("entity")
			return runRecordCreate(app, entity, jsonFile, profile)
		},
	}

	cmd.Flags().StringVar(&jsonFile, "json", "", "path to JSON file containing record data (required)")
	_ = cmd.MarkFlagRequired("json")
	cmd.Flags().StringVar(&profile, "profile", "default", "credentials profile to use")
	return cmd
}

func runRecordCreate(app, entity, jsonFile, profile string) error {
	client, err := newClientFromProfile(profile)
	if err != nil {
		return err
	}

	data, err := loadRecordData(jsonFile)
	if err != nil {
		return err
	}

	recordID, err := client.CreateRecord(app, entity, data)
	if err != nil {
		return err
	}

	fmt.Printf("Record created successfully (recordID: %s)\n", recordID)
	return nil
}

// loadRecordData 读取 JSON 文件并解析为动态 KV map
func loadRecordData(path string) (map[string]any, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 JSON 文件失败: %w", err)
	}
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("JSON 文件格式错误: %w", err)
	}
	return data, nil
}
