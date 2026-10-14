/**
 * [INPUT]: 依赖 internal/agent、resolveAgentGatewayServerURL、firstNonEmpty、errors、os、os/signal、syscall、github.com/spf13/cobra
 * [OUTPUT]: 包内 newAgentRunCmd / runAgent、agentRunOptions 和 errAgentTokenMissing
 * [POS]: cmd/agent run 的隐藏本地聊天与编程入口；独立持有运行参数，远端配置管理命令不依赖这些参数
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
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

type agentRunOptions struct {
	gatewayServerURL, token, model, prompt, system string
	approve, chatOnly                              bool
}

var errAgentTokenMissing = errors.New("缺少平台 token: 传 --token 或设置 MAKE_AGENT_TOKEN")

func newAgentRunCmd() *cobra.Command {
	var o agentRunOptions
	cmd := &cobra.Command{
		Use: "run", Short: "Run a local coding or chat agent", Args: cobra.NoArgs, SilenceUsage: true,
		Hidden:  true,
		Example: "  makecli agent run\n  makecli agent run -p \"Review this project\"\n  makecli agent run --chat-only -p \"Hello\"",
		RunE:    func(cmd *cobra.Command, args []string) error { return runAgent(cmd, o) },
	}
	cmd.Flags().StringVar(&o.gatewayServerURL, "gateway-server-url", "", "Agent 平台 gateway 地址(缺省 MAKE_AGENT_SERVER_URL,再缺省按 --context preset)")
	cmd.Flags().StringVar(&o.token, "token", "", "平台 token(缺省读 MAKE_AGENT_TOKEN)")
	cmd.Flags().StringVar(&o.model, "model", "default", "模型别名(平台侧解析,非厂商模型名)")
	cmd.Flags().StringVarP(&o.prompt, "prompt", "p", "", "一次性模式:发送单条 prompt 后退出")
	cmd.Flags().StringVar(&o.system, "system", "", "system prompt")
	cmd.Flags().BoolVar(&o.approve, "approve", false, "启动即授予当前目录会话信任,副作用工具(bash/write/edit)免逐次确认")
	cmd.Flags().BoolVar(&o.chatOnly, "chat-only", false, "纯聊天模式")
	return cmd
}

func runAgent(cmd *cobra.Command, o agentRunOptions) error {
	token := firstNonEmpty(o.token, os.Getenv("MAKE_AGENT_TOKEN"))
	if token == "" {
		return errAgentTokenMissing
	}
	gatewayURL := o.gatewayServerURL
	if gatewayURL == "" {
		resolved, err := resolveAgentGatewayServerURL()
		if err != nil {
			return err
		}
		gatewayURL = resolved
	}
	ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if o.chatOnly {
		client := agent.NewClient(gatewayURL, token, agent.NewSessionID())
		if o.prompt != "" {
			return agent.RunOnce(ctx, client, o.model, o.system, o.prompt, cmd.OutOrStdout())
		}
		return agent.RunREPL(ctx, client, o.model, o.system, cmd.InOrStdin(), cmd.OutOrStdout())
	}
	opts := agent.CodeOptions{
		GatewayURL: gatewayURL, Token: token, Model: o.model, System: o.system, Approve: o.approve,
	}
	if o.prompt != "" {
		return agent.RunCodeOnce(ctx, opts, o.prompt, cmd.OutOrStdout())
	}
	return agent.RunCodeREPL(ctx, opts, cmd.InOrStdin(), cmd.OutOrStdout())
}
