/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、cmd/record_create（loadRecordData）、fmt、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newRecordUpdateCmd 函数
 * [POS]: cmd/record 的 update 子命令，支持单条和批量更新
 *
 * 路由逻辑备注:
 *   1 个 recordID  -> 调用 UpdateRecord（POST /data/v1/record）— 单条更新，可改多个字段
 *   N 个 recordID -> 调用 UpdateRecordsBatch（POST /data/v1/field）— 批量更新，同一组 KV 应用到所有记录
 *   CLI 根据参数数量自动选择 API 端点，用户无需感知底层差异。
 *
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRecordUpdateCmd() *cobra.Command {
	var profile string
	var jsonFile string

	cmd := &cobra.Command{
		Use:   "update <record-id> [record-id...]",
		Short: "Update one or more records (single ID → record API, multiple IDs → batch field API)",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, _ := cmd.Parent().Flags().GetString("app")
			entity, _ := cmd.Parent().Flags().GetString("entity")
			return runRecordUpdate(app, entity, args, jsonFile, profile)
		},
	}

	cmd.Flags().StringVar(&jsonFile, "json", "", "path to JSON file containing update data (required)")
	_ = cmd.MarkFlagRequired("json")
	cmd.Flags().StringVar(&profile, "profile", "default", "credentials profile to use")
	return cmd
}

func runRecordUpdate(app, entity string, recordIDs []string, jsonFile, profile string) error {
	client, err := newClientFromProfile(profile)
	if err != nil {
		return err
	}

	data, err := loadRecordData(jsonFile)
	if err != nil {
		return err
	}

	// 路由逻辑: 1 个 ID 走单条 API，多个 ID 走批量 API
	if len(recordIDs) == 1 {
		if err := client.UpdateRecord(app, entity, recordIDs[0], data); err != nil {
			return err
		}
		fmt.Printf("Record '%s' updated successfully\n", recordIDs[0])
	} else {
		if err := client.UpdateRecordsBatch(app, entity, recordIDs, data); err != nil {
			return err
		}
		fmt.Printf("%d records updated successfully\n", len(recordIDs))
	}
	return nil
}
