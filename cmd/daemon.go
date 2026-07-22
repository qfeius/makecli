/**
 * [INPUT]: 依赖 github.com/spf13/cobra、os、os/signal、path/filepath、syscall、time、log/slog、internal/daemon（主循环）与 internal/daemon/adapter（claude-code / codex backend）
 * [OUTPUT]: 对外提供 daemonCmd——`makecli daemon` 子命令（Hidden：功能未稳定，不对普通用户展示）
 * [POS]: cmd 模块的设备接入入口：外接 brain 的 daemon 模式——注册设备、claim 领工作、驱动本机 CLI 执行；
 *        配置 flag > env，token 走 --token / MAKE_AGENT_DEVICE_TOKEN（enrollment 流程随 console 进入）
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/qfeius/makecli/internal/daemon"
	"github.com/qfeius/makecli/internal/daemon/adapter"
	"github.com/spf13/cobra"
)

var (
	daemonGatewayURL     string
	daemonToken          string
	daemonDeviceName     string
	daemonWorkDir        string
	daemonMaxRunDuration time.Duration
)

// daemonCmd 是外接 brain 的接入点（agent-design/Design.md §8.1）。
// Hidden：功能未稳定，不在 help 中对普通用户展示；稳定后摘除。
var daemonCmd = &cobra.Command{
	Use:    "daemon",
	Short:  "以设备模式接入 Agent 平台,驱动本机 coding CLI 执行任务",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		token := daemonToken
		if token == "" {
			token = os.Getenv("MAKE_AGENT_DEVICE_TOKEN")
		}
		gatewayURL, err := resolveAgentGatewayURL()
		if err != nil {
			return err
		}
		deviceName := daemonDeviceName
		if deviceName == "" {
			deviceName, _ = os.Hostname()
		}
		workDir := daemonWorkDir
		if workDir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			workDir = filepath.Join(home, ".make", "agent", "work")
		}

		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		agentDaemon, err := daemon.New(ctx, daemon.Options{
			GatewayURL:     gatewayURL,
			Token:          token,
			DeviceName:     deviceName,
			WorkBaseDir:    workDir,
			MaxRunDuration: daemonMaxRunDuration,
			Backends:       []adapter.Backend{&adapter.ClaudeCode{}, &adapter.Codex{}},
			Logger:         logger,
		})
		if err != nil {
			return err
		}
		return agentDaemon.Run(ctx)
	},
}

// resolveAgentGatewayURL 收口 gateway 地址取值链：
// --gateway-url flag > env MAKE_AGENT_GATEWAY_URL > 环境 preset（随全局 --env）。
// 与其余子命令的 URL 解析纪律一致——用户缺省零配置连对环境。
func resolveAgentGatewayURL() (string, error) {
	if daemonGatewayURL != "" {
		return daemonGatewayURL, nil
	}
	if fromEnv := os.Getenv("MAKE_AGENT_GATEWAY_URL"); fromEnv != "" {
		return fromEnv, nil
	}
	environment, err := resolveEnvironment()
	if err != nil {
		return "", err
	}
	return environment.AgentGatewayURL, nil
}

func init() {
	daemonCmd.Flags().StringVar(&daemonGatewayURL, "gateway-url", "", "Agent 平台 gateway 地址(缺省 MAKE_AGENT_GATEWAY_URL,再缺省按 --env 环境 preset)")
	daemonCmd.Flags().StringVar(&daemonToken, "token", "", "设备 token(缺省读 MAKE_AGENT_DEVICE_TOKEN)")
	daemonCmd.Flags().StringVar(&daemonDeviceName, "name", "", "设备名(缺省取 hostname)")
	daemonCmd.Flags().StringVar(&daemonWorkDir, "work-dir", "", "工作目录根(缺省 ~/.make/agent/work)")
	daemonCmd.Flags().DurationVar(&daemonMaxRunDuration, "max-run-duration", daemon.DefaultMaxRunDuration, "单 run 时长兜底")
	rootCmd.AddCommand(daemonCmd)
}
