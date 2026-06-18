/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、internal/api（WithDryRun）、encoding/json、fmt、os、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newRecordCreateCmd 函数、loadRecordData 辅助函数
 * [POS]: cmd/record 的 create 子命令，从 JSON 文件加载数据，调用 Data Service API 创建 Record；--dry-run 经 api.WithDryRun 注入 X-Dry-Run 让远端校验不落库，成功打印 would-be 行（不展示回滚事务的 recordID）
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

func newRecordCreateCmd() *cobra.Command {
	var jsonFile string
	var dryRun bool

	cmd := &cobra.Command{
		Use:          "create",
		Short:        "Create a new record in an entity",
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			appKey, _ := cmd.Parent().Flags().GetString("app")
			entityKey, _ := cmd.Parent().Flags().GetString("entity")
			return runRecordCreate(appKey, entityKey, jsonFile, dryRun)
		},
	}

	cmd.Flags().StringVar(&jsonFile, "json", "", "path to JSON file containing record data (required)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "validate creation on Make without persisting")
	_ = cmd.MarkFlagRequired("json")
	return cmd
}

func runRecordCreate(appKey, entityKey, jsonFile string, dryRun bool) error {
	client, err := newClientFromProfile(api.WithDryRun(dryRun))
	if err != nil {
		return err
	}

	data, err := loadRecordData(jsonFile)
	if err != nil {
		return err
	}

	// dry-run 时服务端回滚事务，返回的 recordID 不指向真实落库记录，故不展示——
	// 只回答「这条记录能不能创建成功」。
	recordID, err := client.CreateRecord(appKey, entityKey, data)
	if err != nil {
		return err
	}

	if dryRun {
		fmt.Printf("Dry run: record would be created successfully in entity '%s' (no changes made)\n", entityKey)
		return nil
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
