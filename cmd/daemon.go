/**
 * [INPUT]: 依赖 github.com/spf13/cobra、context、os、os/signal、path/filepath、syscall、time、log/slog、internal/config、internal/daemon（主循环）与 internal/daemon/adapter（claude-code / codex backend）
 * [OUTPUT]: 对外提供 daemonCmd——`makecli daemon` 子命令（Hidden：功能未稳定，不对普通用户展示）；
 *           包内提供 daemonRunConfig / resolveDaemonRunConfig / newEnrolledDaemon（前台运行与 launchd 托管共用）
 * [POS]: cmd 模块的设备接入入口：外接 brain 的 daemon 模式——注册设备、claim 领工作、驱动本机 CLI 执行；
 *        配置 flag > env；首次入册走 --setup-key（console 铸），换回 node key 落 credentials；重启读本地 node key 续连
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"context"
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
// 自身 RunE = 前台运行（launchd 托管时被 plist 直接调用的就是这一形态），
// start/stop/restart/status 子命令是它的 macOS 托管面（daemon_service.go）。
// Hidden：功能未稳定，不在 help 中对普通用户展示；稳定后摘除。
var daemonCmd = &cobra.Command{
	Use:    "daemon",
	Short:  "以设备模式接入 Agent 平台,驱动本机 coding CLI 执行任务",
	Hidden: true,
	// NoArgs 让 `makecli daemon statsu` 这类拼错直接报"未知子命令"，
	// 而不是被当成位置参数悄悄起了一个前台 daemon。
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
		runConfig, err := resolveDaemonRunConfig()
		if err != nil {
			return err
		}

		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		agentDaemon, err := newEnrolledDaemon(ctx, runConfig, logger)
		if err != nil {
			return err
		}
		return agentDaemon.Run(ctx)
	},
}

// daemonRunConfig 是 daemon 启动事实的解析结果。
// 收口成一个类型是因为 launchd 托管必须把这些值固化进 plist：
// launchd 拉起的进程没有用户 shell 环境，env 与 preset 的兜底届时都不在了。
type daemonRunConfig struct {
	ServerURL      string
	SetupKey       string
	RuntimeName    string
	WorkDir        string
	MaxRunDuration time.Duration
}

// resolveDaemonRunConfig 把 flag / env / 环境 preset 解析成最终值（缺省一次性补齐）。
func resolveDaemonRunConfig() (daemonRunConfig, error) {
	serverURL, err := resolveAgentGatewayServerURL()
	if err != nil {
		return daemonRunConfig{}, err
	}
	runtimeName := daemonRuntimeName
	if runtimeName == "" {
		runtimeName, _ = os.Hostname()
	}
	workDir := daemonWorkDir
	if workDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return daemonRunConfig{}, err
		}
		workDir = filepath.Join(home, ".make", "agent", "work")
	}
	maxRunDuration := daemonMaxRunDuration
	if maxRunDuration <= 0 {
		maxRunDuration = daemon.DefaultMaxRunDuration
	}
	return daemonRunConfig{
		ServerURL:      serverURL,
		SetupKey:       daemonSetupKey,
		RuntimeName:    runtimeName,
		WorkDir:        workDir,
		MaxRunDuration: maxRunDuration,
	}, nil
}

// newEnrolledDaemon 构造 daemon 并收口入册副作用：
// 复用本地已入册的 node key（重启即续连），无则用 --setup-key 首次入册，
// 换回的新 node key 合并写回 credentials（保留同段 access_token）。
func newEnrolledDaemon(ctx context.Context, runConfig daemonRunConfig, logger *slog.Logger) (*daemon.Daemon, error) {
	creds, err := config.Load()
	if err != nil {
		return nil, err
	}
	nodeKey := creds[Profile].NodeKey

	agentDaemon, err := daemon.New(ctx, daemon.Options{
		ServerURL:      runConfig.ServerURL,
		NodeKey:        nodeKey,
		SetupKey:       runConfig.SetupKey,
		RuntimeName:    runConfig.RuntimeName,
		WorkBaseDir:    runConfig.WorkDir,
		MaxRunDuration: runConfig.MaxRunDuration,
		Backends:       []adapter.Backend{&adapter.ClaudeCode{}, &adapter.Codex{}},
		Logger:         logger,
	})
	if err != nil {
		return nil, err
	}
	if agentDaemon.NodeKey() != nodeKey {
		profile := creds[Profile]
		profile.NodeKey = agentDaemon.NodeKey()
		creds[Profile] = profile
		if err := config.Save(creds); err != nil {
			return nil, fmt.Errorf("持久化 node key 失败: %w", err)
		}
		logger.Info("node key 已保存到 credentials", "profile", Profile)
	}
	return agentDaemon, nil
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
	// PersistentFlags：start 子命令要用完全同一套旋钮把前台形态固化进 plist，
	// 定义一次即父子共享，杜绝两处 flag 定义漂移。
	daemonCmd.PersistentFlags().StringVar(&daemonGatewayServerURL, "gateway-server-url", "", "Agent 平台 gateway 地址(缺省 MAKE_AGENT_SERVER_URL,再缺省按 --env 环境 preset)")
	daemonCmd.PersistentFlags().StringVar(&daemonSetupKey, "setup-key", "", "首次入册用的一次性 setup-key(已入册则读本地 node key,无需再传)")
	daemonCmd.PersistentFlags().StringVar(&daemonRuntimeName, "name", "", "runtime 名(缺省取 hostname)")
	daemonCmd.PersistentFlags().StringVar(&daemonWorkDir, "work-dir", "", "工作目录根(缺省 ~/.make/agent/work)")
	daemonCmd.PersistentFlags().DurationVar(&daemonMaxRunDuration, "max-run-duration", daemon.DefaultMaxRunDuration, "单 run 时长兜底")
	rootCmd.AddCommand(daemonCmd)
}
