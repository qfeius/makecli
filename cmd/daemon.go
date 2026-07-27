/**
 * [INPUT]: 依赖 github.com/spf13/cobra、os、os/signal、path/filepath、syscall、time、log/slog、internal/daemon（主循环）与 internal/daemon/adapter（claude-code / codex backend）
 * [OUTPUT]: 对外提供 daemonCmd——`makecli daemon` 子命令（Hidden：功能未稳定，不对普通用户展示）
 * [POS]: cmd 模块的设备接入入口：外接 brain 的 daemon 模式——注册设备、claim 领工作、驱动本机 CLI 执行；
 *        配置 flag > env；首次入册走 --setup-key（console 铸），换回 node key 落 credentials；重启读本地 node key 续连
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/qfeius/makecli/internal/config"
	"github.com/qfeius/makecli/internal/daemon"
	"github.com/qfeius/makecli/internal/daemon/adapter"
	"github.com/spf13/cobra"
)

var (
	daemonGatewayServerURL string
	daemonSetupKey         string
	daemonRuntimeName      string
	daemonWorkDir          string
	daemonMaxRunDuration   time.Duration
)

// daemonCmd 是外接 brain 的接入点（agent-design/Design.md §8.1）。
// Hidden：功能未稳定，不在 help 中对普通用户展示；稳定后摘除。
var daemonCmd = &cobra.Command{
	Use:    "daemon",
	Short:  "以设备模式接入 Agent 平台,驱动本机 coding CLI 执行任务",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		serverURL, err := resolveAgentGatewayServerURL()
		if err != nil {
			return err
		}
		runtimeName := daemonRuntimeName
		if runtimeName == "" {
			runtimeName, _ = os.Hostname()
		}
		workDir := daemonWorkDir
		if workDir == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			workDir = filepath.Join(home, ".make", "agent", "work")
		}

		// 复用本地已入册的 node key（重启即续连）；无则用 --setup-key 首次入册。
		creds, err := config.Load()
		if err != nil {
			return err
		}
		nodeKey := creds[Profile].NodeKey

		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		agentDaemon, err := daemon.New(ctx, daemon.Options{
			ServerURL:      serverURL,
			NodeKey:        nodeKey,
			SetupKey:       daemonSetupKey,
			RuntimeName:    runtimeName,
			WorkBaseDir:    workDir,
			MaxRunDuration: daemonMaxRunDuration,
			Backends:       []adapter.Backend{&adapter.ClaudeCode{}, &adapter.Codex{}},
			Logger:         logger,
		})
		if err != nil {
			return err
		}
		// 首次入册换回新 node key：合并写回 credentials（保留同段 access_token）。
		if agentDaemon.NodeKey() != nodeKey {
			profile := creds[Profile]
			profile.NodeKey = agentDaemon.NodeKey()
			creds[Profile] = profile
			if err := config.Save(creds); err != nil {
				return fmt.Errorf("持久化 node key 失败: %w", err)
			}
			logger.Info("node key 已保存到 credentials", "profile", Profile)
		}
		return agentDaemon.Run(ctx)
	},
}

// resolveAgentGatewayServerURL 收口 gateway 地址取值链：
// --gateway-server-url flag > env MAKE_AGENT_SERVER_URL > 环境 preset（随全局 --env）。
// 与其余子命令的 URL 解析纪律一致——用户缺省零配置连对环境。
func resolveAgentGatewayServerURL() (string, error) {
	if daemonGatewayServerURL != "" {
		return daemonGatewayServerURL, nil
	}
	if fromEnv := os.Getenv("MAKE_AGENT_SERVER_URL"); fromEnv != "" {
		return fromEnv, nil
	}
	environment, err := resolveEnvironment()
	if err != nil {
		return "", err
	}
	return environment.AgentGatewayURL, nil
}

func init() {
	daemonCmd.Flags().StringVar(&daemonGatewayServerURL, "gateway-server-url", "", "Agent 平台 gateway 地址(缺省 MAKE_AGENT_SERVER_URL,再缺省按 --env 环境 preset)")
	daemonCmd.Flags().StringVar(&daemonSetupKey, "setup-key", "", "首次入册用的一次性 setup-key(已入册则读本地 node key,无需再传)")
	daemonCmd.Flags().StringVar(&daemonRuntimeName, "name", "", "runtime 名(缺省取 hostname)")
	daemonCmd.Flags().StringVar(&daemonWorkDir, "work-dir", "", "工作目录根(缺省 ~/.make/agent/work)")
	daemonCmd.Flags().DurationVar(&daemonMaxRunDuration, "max-run-duration", daemon.DefaultMaxRunDuration, "单 run 时长兜底")
	rootCmd.AddCommand(daemonCmd)
}
