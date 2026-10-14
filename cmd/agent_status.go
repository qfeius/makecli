/**
 * [INPUT]: 依赖 cmd/client、validResourceKey、validAgentAppKey、cmd/output、fmt、github.com/spf13/cobra
 * [OUTPUT]: 包内 newAgentStatusCmd / runAgentStatus，共用 enable/disable 命令构造和执行
 * [POS]: cmd/agent 的启停子命令，仅更新实际应用内 Agent 的 status，其余配置由服务端保持
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newAgentStatusCmd(action, status string) *cobra.Command {
	var appKey, output string
	cmd := &cobra.Command{
		Use:          action + " <agentKey>",
		Short:        fmt.Sprintf("%s an agent on Make app", map[string]string{"enable": "Enable", "disable": "Disable"}[action]),
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		Example:      fmt.Sprintf("  makecli agent %s poc_agent --app agent_poc", action),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAgentStatus(args[0], appKey, status, output)
		},
	}
	cmd.Flags().StringVar(&appKey, "app", "", "actual environment app key (required)")
	_ = cmd.MarkFlagRequired("app")
	addOutputFlag(cmd, &output)
	return cmd
}

func runAgentStatus(key, appKey, status, output string) error {
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
	client, err := newClientFromProfile()
	if err != nil {
		return err
	}
	saved, err := client.SetAgentStatus(key, appKey, status)
	if err != nil {
		return err
	}
	if output == outputJSON {
		return writeJSON(saved)
	}
	fmt.Printf("Agent '%s' %s successfully in app '%s'\n", saved.Key, saved.Status, saved.AppKey)
	return nil
}
