/**
 * [INPUT]: 依赖 github.com/spf13/cobra、os、os/signal、syscall、internal/agent（传输 + 会话编排）
 * [OUTPUT]: 对外提供 agentCmd——`makecli agent` 子命令（Hidden：keyless 通道未公开，不对普通用户展示）
 * [POS]: cmd 模块的自营脑设备版入口（agent-design/Design.md §8.2）：LLM 走平台——
 *        token 只开模型门，设备端零厂商 key；gateway 地址取值链与 daemon 一致
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/qfeius/makecli/internal/agent"
	"github.com/spf13/cobra"
)

var (
	agentGatewayURL   string
	agentToken        string
	agentModel        string
	agentPrompt       string
	agentSystemPrompt string
)

// agentCmd 是 keyless 本地 agent（自营脑设备版，Design.md §8.2）。
// Hidden：功能未稳定，不在 help 中对普通用户展示；稳定后摘除。
var agentCmd = &cobra.Command{
	Use:          "agent",
	Short:        "keyless 本地 agent:LLM 走平台,设备端零厂商 key",
	Hidden:       true,
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		token := agentToken
		if token == "" {
			token = os.Getenv("MAKE_AGENT_TOKEN")
		}
		if token == "" {
			return errAgentTokenMissing
		}
		gatewayURL := agentGatewayURL
		if gatewayURL == "" {
			resolved, err := resolveAgentGatewayURL()
			if err != nil {
				return err
			}
			gatewayURL = resolved
		}

		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		client := agent.NewClient(gatewayURL, token, agent.NewSessionID())
		if agentPrompt != "" {
			return agent.RunOnce(ctx, client, agentModel, agentSystemPrompt, agentPrompt, cmd.OutOrStdout())
		}
		return agent.RunREPL(ctx, client, agentModel, agentSystemPrompt, cmd.InOrStdin(), cmd.OutOrStdout())
	},
}

// errAgentTokenMissing 是可操作的缺 token 报错（引导补救而非堆栈）。
var errAgentTokenMissing = errors.New("缺少平台 token: 传 --token 或设置 MAKE_AGENT_TOKEN")

func init() {
	agentCmd.Flags().StringVar(&agentGatewayURL, "gateway-url", "", "Agent 平台 gateway 地址(缺省 MAKE_AGENT_GATEWAY_URL,再缺省按 --env 环境 preset)")
	agentCmd.Flags().StringVar(&agentToken, "token", "", "平台 token(缺省读 MAKE_AGENT_TOKEN)")
	agentCmd.Flags().StringVar(&agentModel, "model", "default", "模型别名(平台侧解析,非厂商模型名)")
	agentCmd.Flags().StringVarP(&agentPrompt, "prompt", "p", "", "一次性模式:发送单条 prompt 后退出")
	agentCmd.Flags().StringVar(&agentSystemPrompt, "system", "", "system prompt")
	rootCmd.AddCommand(agentCmd)
}
