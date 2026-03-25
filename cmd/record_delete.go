/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、fmt、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newRecordDeleteCmd 函数
 * [POS]: cmd/record 的 delete 子命令，调用 Data Service API 批量删除 Record
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newRecordDeleteCmd() *cobra.Command {
	var profile string

	cmd := &cobra.Command{
		Use:          "delete <record-id> [record-id...]",
		Short:        "Delete one or more records",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, _ := cmd.Parent().Flags().GetString("app")
			entity, _ := cmd.Parent().Flags().GetString("entity")
			return runRecordDelete(app, entity, args, profile)
		},
	}

	cmd.Flags().StringVar(&profile, "profile", "default", "credentials profile to use")
	return cmd
}

func runRecordDelete(app, entity string, recordIDs []string, profile string) error {
	client, err := newClientFromProfile(profile)
	if err != nil {
		return err
	}

	results, err := client.DeleteRecords(app, entity, recordIDs)
	if err != nil {
		return err
	}

	// 汇报每条记录的删除结果
	var failed int
	for _, r := range results {
		if r.Code != 200 {
			fmt.Printf("  FAIL  %s: %s\n", r.RecordID, r.Message)
			failed++
		}
	}

	if failed > 0 {
		return fmt.Errorf("%d of %d records failed to delete", failed, len(results))
	}

	fmt.Printf("%d record(s) deleted successfully\n", len(recordIDs))
	return nil
}
