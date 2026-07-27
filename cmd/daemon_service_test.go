/**
 * [INPUT]: 依赖 context、encoding/json、log/slog、path/filepath、strings、testing、internal/daemon、internal/daemon/launchd；
 *          复用 captureStdout / setProfile（stdout_test.go）
 * [OUTPUT]: 对外提供 daemon start / stop / restart / status 的单元测试
 * [POS]: cmd 模块的 daemon 托管面测试——打桩 launchd 原语与入册前置，非 macOS 机器也能跑全路径
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/qfeius/makecli/internal/daemon"
	"github.com/qfeius/makecli/internal/daemon/launchd"
)

// setHostGOOS 临时覆盖平台守卫的判定源。
func setHostGOOS(t *testing.T, goos string) {
	t.Helper()
	original := hostGOOS
	hostGOOS = goos
	t.Cleanup(func() { hostGOOS = original })
}

// setDaemonFlags 临时覆盖 daemon flag 全局态（cobra 未参与解析时直接赋值）。
func setDaemonFlags(t *testing.T, serverURL, runtimeName, workDir string) {
	t.Helper()
	originalURL, originalName, originalDir, originalMax := daemonGatewayServerURL, daemonRuntimeName, daemonWorkDir, daemonMaxRunDuration
	daemonGatewayServerURL, daemonRuntimeName, daemonWorkDir = serverURL, runtimeName, workDir
	daemonMaxRunDuration = daemon.DefaultMaxRunDuration
	t.Cleanup(func() {
		daemonGatewayServerURL, daemonRuntimeName, daemonWorkDir, daemonMaxRunDuration = originalURL, originalName, originalDir, originalMax
	})
}

// stubEnroll 打桩入册前置，记录是否被调用。
func stubEnroll(t *testing.T, err error) *bool {
	t.Helper()
	called := false
	original := newEnrolledDaemonFunc
	newEnrolledDaemonFunc = func(context.Context, daemonRunConfig, *slog.Logger) (*daemon.Daemon, error) {
		called = true
		return nil, err
	}
	t.Cleanup(func() { newEnrolledDaemonFunc = original })
	return &called
}

// stubInstall 打桩 LaunchAgent 安装，捕获落到 plist 的配置。
func stubInstall(t *testing.T, err error) *[]launchd.Config {
	t.Helper()
	captured := &[]launchd.Config{}
	original := launchdInstall
	launchdInstall = func(config launchd.Config) (string, error) {
		*captured = append(*captured, config)
		return "/Users/tester/Library/LaunchAgents/" + launchd.Label + ".plist", err
	}
	t.Cleanup(func() { launchdInstall = original })
	return captured
}

func stubStop(t *testing.T, installed bool, err error) {
	t.Helper()
	original := launchdStop
	launchdStop = func() (bool, error) { return installed, err }
	t.Cleanup(func() { launchdStop = original })
}

func stubUninstall(t *testing.T, installed bool, err error) {
	t.Helper()
	original := launchdUninstall
	launchdUninstall = func() (bool, error) { return installed, err }
	t.Cleanup(func() { launchdUninstall = original })
}

func stubRestart(t *testing.T, err error) {
	t.Helper()
	original := launchdRestart
	launchdRestart = func() error { return err }
	t.Cleanup(func() { launchdRestart = original })
}

func stubQuery(t *testing.T, status launchd.Status, err error) {
	t.Helper()
	original := launchdQuery
	launchdQuery = func() (launchd.Status, error) { return status, err }
	t.Cleanup(func() { launchdQuery = original })
}

func setDaemonStatusOutput(t *testing.T, format string) {
	t.Helper()
	original := daemonStatusOutput
	daemonStatusOutput = format
	t.Cleanup(func() { daemonStatusOutput = original })
}

func TestDaemonStartInstallsResolvedConfig(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("MAKE_CLI_CONFIG_DIR", configDir)
	setHostGOOS(t, "darwin")
	setProfile(t, "work")
	setDaemonFlags(t, "https://dev-make-agent.qtech.cn", "mac-mini", "/tmp/agent-work")
	enrolled := stubEnroll(t, nil)
	captured := stubInstall(t, nil)

	output := captureStdout(t, func() {
		if err := runDaemonStart(context.Background()); err != nil {
			t.Fatalf("runDaemonStart: %v", err)
		}
	})

	if !*enrolled {
		t.Fatal("start 必须先跑入册/探测前置")
	}
	if len(*captured) != 1 {
		t.Fatalf("应安装一次 LaunchAgent，实际 %d 次", len(*captured))
	}
	config := (*captured)[0]

	wantArgs := []string{
		"--profile", "work",
		"daemon",
		"--gateway-server-url", "https://dev-make-agent.qtech.cn",
		"--name", "mac-mini",
		"--work-dir", "/tmp/agent-work",
		"--max-run-duration", daemon.DefaultMaxRunDuration.String(),
	}
	if strings.Join(config.Args, " ") != strings.Join(wantArgs, " ") {
		t.Fatalf("plist 参数 = %v, want %v", config.Args, wantArgs)
	}
	if config.BinaryPath == "" {
		t.Fatal("必须写入 makecli 绝对路径")
	}
	// launchd 只给最小 PATH：不带 PATH 就探测不到 claude / codex。
	if config.Env["PATH"] == "" {
		t.Fatal("PATH 必须固化进 plist")
	}
	if config.Env["MAKE_CLI_CONFIG_DIR"] != configDir {
		t.Fatalf("自定义配置目录应随托管带上: %q", config.Env["MAKE_CLI_CONFIG_DIR"])
	}
	if config.LogPath != filepath.Join(configDir, "agent", "logs", "daemon.log") {
		t.Fatalf("日志落点 = %q", config.LogPath)
	}
	if !strings.Contains(output, "mac-mini") || !strings.Contains(output, "launchd") {
		t.Fatalf("应回显 runtime 与托管结论，实际:\n%s", output)
	}
}

func TestDaemonStartAbortsWhenPreflightFails(t *testing.T) {
	t.Setenv("MAKE_CLI_CONFIG_DIR", t.TempDir())
	setHostGOOS(t, "darwin")
	setDaemonFlags(t, "https://dev-make-agent.qtech.cn", "mac-mini", "/tmp/agent-work")
	stubEnroll(t, errors.New("没有可用的 brain CLI（claude / codex 均未探测到）"))
	captured := stubInstall(t, nil)

	err := runDaemonStart(context.Background())
	if err == nil {
		t.Fatal("前置失败必须上抛，不能把错误埋进 launchd 重启循环")
	}
	if !strings.Contains(err.Error(), "brain CLI") {
		t.Fatalf("应原样透传前置错误，实际: %v", err)
	}
	if len(*captured) != 0 {
		t.Fatal("前置失败时不得安装 LaunchAgent")
	}
}

func TestDaemonServiceCommandsRejectNonDarwin(t *testing.T) {
	setHostGOOS(t, "linux")
	captured := stubInstall(t, nil)
	stubEnroll(t, nil)
	stubStop(t, true, nil)
	stubUninstall(t, true, nil)
	stubRestart(t, nil)
	stubQuery(t, launchd.Status{}, nil)
	setDaemonStatusOutput(t, outputTable)

	actions := map[string]func() error{
		"start":     func() error { return runDaemonStart(context.Background()) },
		"stop":      runDaemonStop,
		"restart":   runDaemonRestart,
		"status":    runDaemonStatus,
		"uninstall": runDaemonUninstall,
	}
	for name, action := range actions {
		err := action()
		if err == nil {
			t.Fatalf("%s 在非 macOS 上必须报错", name)
		}
		if !strings.Contains(err.Error(), "macOS") || !strings.Contains(err.Error(), "makecli daemon") {
			t.Fatalf("%s 的错误应说明只支持 macOS 并指回前台形态，实际: %v", name, err)
		}
	}
	if len(*captured) != 0 {
		t.Fatal("平台守卫应在任何副作用之前短路")
	}
}

func TestDaemonStopKeepsAgent(t *testing.T) {
	setHostGOOS(t, "darwin")
	stubStop(t, true, nil)

	output := captureStdout(t, func() {
		if err := runDaemonStop(); err != nil {
			t.Fatalf("runDaemonStop: %v", err)
		}
	})
	if !strings.Contains(output, "已停止") || !strings.Contains(output, "保留") {
		t.Fatalf("应说明停了但托管配置还在，实际: %s", output)
	}
	// stop 与 uninstall 是两个动词，输出要把去向讲清楚。
	if !strings.Contains(output, "makecli daemon uninstall") {
		t.Fatalf("应指出彻底移除的走法，实际: %s", output)
	}
}

func TestDaemonStopIsIdempotent(t *testing.T) {
	setHostGOOS(t, "darwin")
	stubStop(t, false, nil)

	output := captureStdout(t, func() {
		if err := runDaemonStop(); err != nil {
			t.Fatalf("未托管时 stop 不该报错: %v", err)
		}
	})
	if !strings.Contains(output, "未托管") {
		t.Fatalf("应说明本就未托管，实际: %s", output)
	}
}

func TestDaemonUninstallRemovesAgent(t *testing.T) {
	t.Setenv("MAKE_CLI_CONFIG_DIR", t.TempDir())
	setHostGOOS(t, "darwin")
	stubUninstall(t, true, nil)

	output := captureStdout(t, func() {
		if err := runDaemonUninstall(); err != nil {
			t.Fatalf("runDaemonUninstall: %v", err)
		}
	})
	if !strings.Contains(output, "已移除") {
		t.Fatalf("应回显移除结论，实际: %s", output)
	}
	// 移除托管 ≠ 删数据：留下什么必须明说，否则用户以为清干净了。
	if !strings.Contains(output, "保留") || !strings.Contains(output, "node key") {
		t.Fatalf("应交代保留的数据，实际: %s", output)
	}
}

func TestDaemonUninstallIsIdempotent(t *testing.T) {
	t.Setenv("MAKE_CLI_CONFIG_DIR", t.TempDir())
	setHostGOOS(t, "darwin")
	stubUninstall(t, false, nil)

	output := captureStdout(t, func() {
		if err := runDaemonUninstall(); err != nil {
			t.Fatalf("未托管时 uninstall 不该报错: %v", err)
		}
	})
	if !strings.Contains(output, "未托管") {
		t.Fatalf("应说明本就未托管，实际: %s", output)
	}
}

func TestDaemonRestartGuidesWhenNotInstalled(t *testing.T) {
	setHostGOOS(t, "darwin")
	stubRestart(t, launchd.ErrNotInstalled)

	err := runDaemonRestart()
	if err == nil {
		t.Fatal("未托管时 restart 应报错")
	}
	if !strings.Contains(err.Error(), "makecli daemon start") {
		t.Fatalf("错误应给出可操作指引，实际: %v", err)
	}
}

func TestDaemonRestartSucceeds(t *testing.T) {
	setHostGOOS(t, "darwin")
	stubRestart(t, nil)

	output := captureStdout(t, func() {
		if err := runDaemonRestart(); err != nil {
			t.Fatalf("runDaemonRestart: %v", err)
		}
	})
	if !strings.Contains(output, "已重启") {
		t.Fatalf("应回显重启结论，实际: %s", output)
	}
}

func runningStatus() launchd.Status {
	return launchd.Status{
		Label:          launchd.Label,
		Installed:      true,
		Loaded:         true,
		Running:        true,
		PID:            4242,
		LastExitStatus: 0,
		HasLastExit:    true,
		PlistPath:      "/Users/tester/Library/LaunchAgents/" + launchd.Label + ".plist",
		Command:        []string{"/usr/local/bin/makecli", "daemon", "--name", "mac-mini"},
		LogPath:        "/Users/tester/.make/agent/logs/daemon.log",
	}
}

func TestDaemonStatusTableRunning(t *testing.T) {
	setHostGOOS(t, "darwin")
	setDaemonStatusOutput(t, outputTable)
	stubQuery(t, runningStatus(), nil)

	output := captureStdout(t, func() {
		if err := runDaemonStatus(); err != nil {
			t.Fatalf("runDaemonStatus: %v", err)
		}
	})
	for _, want := range []string{launchd.Label, "running", "4242", "daemon.log", "--name mac-mini", "Autostart", "enabled"} {
		if !strings.Contains(output, want) {
			t.Fatalf("状态输出缺 %q:\n%s", want, output)
		}
	}
}

func TestDaemonStatusDistinguishesStoppedFromDisabled(t *testing.T) {
	setHostGOOS(t, "darwin")
	setDaemonStatusOutput(t, outputTable)
	status := runningStatus()
	status.Running = false
	status.PID = 0
	status.Disabled = true
	stubQuery(t, status, nil)

	output := captureStdout(t, func() {
		if err := runDaemonStatus(); err != nil {
			t.Fatalf("runDaemonStatus: %v", err)
		}
	})
	// 「现在没跑」和「登录也不会自己回来」是两件事，用户要能一眼分开。
	if !strings.Contains(output, "disabled") {
		t.Fatalf("被 stop 停用应回显 Autostart disabled:\n%s", output)
	}
	if !strings.Contains(output, "daemon stop") || !strings.Contains(output, "daemon start") {
		t.Fatalf("应说明是被 stop 停用的并给出重新拉起的走法:\n%s", output)
	}
}

func TestDaemonStatusTableStoppedGivesNextStep(t *testing.T) {
	setHostGOOS(t, "darwin")
	setDaemonStatusOutput(t, outputTable)
	status := runningStatus()
	status.Running = false
	status.PID = 0
	status.LastExitStatus = 1
	stubQuery(t, status, nil)

	output := captureStdout(t, func() {
		if err := runDaemonStatus(); err != nil {
			t.Fatalf("runDaemonStatus: %v", err)
		}
	})
	if !strings.Contains(output, daemonStateStopped) {
		t.Fatalf("已托管但没进程应为 stopped:\n%s", output)
	}
	if !strings.Contains(output, "Last exit") || !strings.Contains(output, "restart") {
		t.Fatalf("应给出退出码与下一步:\n%s", output)
	}
}

func TestDaemonStatusNotInstalled(t *testing.T) {
	setHostGOOS(t, "darwin")
	setDaemonStatusOutput(t, outputTable)
	stubQuery(t, launchd.Status{Label: launchd.Label, PlistPath: "/Users/tester/Library/LaunchAgents/x.plist"}, nil)

	output := captureStdout(t, func() {
		if err := runDaemonStatus(); err != nil {
			t.Fatalf("runDaemonStatus: %v", err)
		}
	})
	if !strings.Contains(output, daemonStateNotInstalled) || !strings.Contains(output, "makecli daemon start") {
		t.Fatalf("未托管应说明状态并给出下一步:\n%s", output)
	}
}

func TestDaemonStatusJSON(t *testing.T) {
	setHostGOOS(t, "darwin")
	setDaemonStatusOutput(t, outputJSON)
	stubQuery(t, runningStatus(), nil)

	output := captureStdout(t, func() {
		if err := runDaemonStatus(); err != nil {
			t.Fatalf("runDaemonStatus: %v", err)
		}
	})
	var result daemonStatusResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("JSON 解析失败: %v\n%s", err, output)
	}
	if result.State != "running" {
		t.Fatalf("state = %q, want running", result.State)
	}
	if result.PID != 4242 || result.LastExitStatus == nil || *result.LastExitStatus != 0 {
		t.Fatalf("PID / LastExitStatus 未到位: %+v", result)
	}
	if len(result.Command) == 0 {
		t.Fatal("JSON 应带上托管命令行")
	}
}

func TestDaemonStatusJSONStateIsMachineReadable(t *testing.T) {
	setHostGOOS(t, "darwin")
	setDaemonStatusOutput(t, outputJSON)
	stubQuery(t, launchd.Status{Label: launchd.Label, PlistPath: "/x.plist"}, nil)

	output := captureStdout(t, func() {
		if err := runDaemonStatus(); err != nil {
			t.Fatalf("runDaemonStatus: %v", err)
		}
	})
	var result daemonStatusResult
	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("JSON 解析失败: %v\n%s", err, output)
	}
	if result.State != "not_installed" {
		t.Fatalf("JSON 的 state 应为下划线形态，实际 %q", result.State)
	}
}

func TestDaemonStatusRejectsBadFormat(t *testing.T) {
	setHostGOOS(t, "darwin")
	setDaemonStatusOutput(t, "yaml")
	stubQuery(t, runningStatus(), nil)

	if err := runDaemonStatus(); err == nil {
		t.Fatal("非法输出格式必须拒绝")
	}
}

func TestDaemonSubcommandsRegistered(t *testing.T) {
	want := map[string]bool{"start": false, "stop": false, "restart": false, "status": false, "uninstall": false}
	for _, sub := range daemonCmd.Commands() {
		if _, ok := want[sub.Name()]; ok {
			want[sub.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("daemon 缺子命令 %q", name)
		}
	}
	// 托管面与前台形态共用同一套 flag（PersistentFlags），start 必须能看到。
	if daemonStartCmd.InheritedFlags().Lookup("gateway-server-url") == nil {
		t.Fatal("start 应继承 daemon 的 --gateway-server-url")
	}
	if daemonMaxRunDuration <= 0 || daemonMaxRunDuration > 24*time.Hour {
		t.Fatalf("max-run-duration 缺省值异常: %v", daemonMaxRunDuration)
	}
}
