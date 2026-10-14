/**
 * [INPUT]: 依赖 fmt、strings、unicode/utf8、github.com/spf13/cobra、agent_{create,list,delete,info,status,run,runtime,model}.go
 * [OUTPUT]: 对外提供 agentCmd；包内 newAgentCmd 和 validAgentAppKey
 * [POS]: cmd/agent 命令组，组织远端配置管理与显式本地 run 入口；无子命令显示帮助
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package cmd

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/spf13/cobra"
)

var agentCmd = newAgentCmd()

func newAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "agent", Short: "Manage agents", Args: cobra.NoArgs, SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error { return cmd.Help() },
	}
	cmd.AddCommand(newAgentCreateCmd(), newAgentListCmd(), newAgentDeleteCmd(), newAgentInfoCmd())
	cmd.AddCommand(newAgentStatusCmd("enable", "enabled"), newAgentStatusCmd("disable", "disabled"), newAgentRunCmd())
	cmd.AddCommand(newAgentRuntimeCmd(), newAgentModelCmd())
	return cmd
}

// validAgentAppKey 统一远端配置命令的应用归属约束，不套用资源 key 的 20 字符限制。
func validAgentAppKey(key string) error {
	if strings.TrimSpace(key) == "" || utf8.RuneCountInString(key) > 128 {
		return fmt.Errorf("--app must contain 1–128 characters and must not be blank")
	}
	return nil
}

func init() {
	rootCmd.AddCommand(agentCmd)
}
