/**
 * [INPUT]: 依赖 cmd/client、cmd/app 的 validResourceKey、cmd/agent 的 validAgentAppKey、cmd/output、delete_confirm.go、fmt、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newAgentDeleteCmd；包内 runAgentDelete、confirmAgentDeleteFunc 确认钩子
 * [POS]: cmd/agent 的远端删除子命令，以 appKey + key 定位，软删除并保留历史；与 app delete 共用输入 key 确认，--yes 跳过
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var confirmAgentDeleteFunc = func(key, appKey string) error {
	return confirmDeletionByTypingKey(key, fmt.Sprintf("agent %q from app %q", key, appKey))
}

func newAgentDeleteCmd() *cobra.Command {
	var appKey, output string
	var yes bool
	cmd := &cobra.Command{
		Use:          "delete <agentKey>",
		Short:        "Delete an agent on Make app",
		Long:         "Soft-delete the agent in the actual environment appKey specified by --app. Historical records are preserved.",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		Example:      "  makecli agent delete invoice_agent --app invoice_app\n  makecli agent delete invoice_agent --app invoice_app --yes",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentDelete(args[0], appKey, output, yes)
		},
	}
	cmd.Flags().StringVar(&appKey, "app", "", "actual environment app key (required)")
	_ = cmd.MarkFlagRequired("app")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the deletion confirmation prompt")
	addOutputFlag(cmd, &output)
	return cmd
}

func runAgentDelete(key, appKey, output string, skipConfirm bool) error {
	output, err := resolveOutputFormat(output)
	if err != nil {
		return err
	}
	if err := validResourceKey(key); err != nil {
		return err
	}
	if err := validAgentAppKey(appKey); err != nil {
		return err
	}
	if !skipConfirm {
		if err := confirmAgentDeleteFunc(key, appKey); err != nil {
			return err
		}
	}
	client, err := newClientFromProfile()
	if err != nil {
		return err
	}
	if err := client.DeleteAgent(key, appKey); err != nil {
		return err
	}
	if output == outputJSON {
		return writeJSON(map[string]any{"key": key, "appKey": appKey, "deleted": true})
	}
	fmt.Printf("Agent '%s' deleted successfully from app '%s'\n", key, appKey)
	return nil
}
