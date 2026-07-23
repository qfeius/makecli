/**
 * [INPUT]: 依赖 agent.go；stdout_test.go 的隔离 helper
 * [OUTPUT]: 对外提供 agent 命令回归——Hidden 不进 help、缺 token 可操作报错、
 *           code agent 新 flag（--approve/--no-tools）注册与缺省值
 * [POS]: cmd 模块的测试面（agent 命令）
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"strings"
	"testing"
)

func TestAgentCommandHiddenFromHelp(t *testing.T) {
	if !agentCmd.Hidden {
		t.Error("agent 命令必须 Hidden")
	}
	var output strings.Builder
	rootCmd.SetOut(&output)
	rootCmd.SetErr(&output)
	t.Cleanup(func() { rootCmd.SetOut(nil); rootCmd.SetErr(nil) })
	rootCmd.SetArgs([]string{"--help"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("help: %v", err)
	}
	if strings.Contains(output.String(), "agent") && strings.Contains(output.String(), "keyless") {
		t.Errorf("help 不应出现 agent 命令:\n%s", output.String())
	}
}

func TestAgentCodeFlagsRegistered(t *testing.T) {
	for name, wantDefault := range map[string]string{
		"approve":  "false",
		"no-tools": "false",
		"model":    "default",
	} {
		flag := agentCmd.Flags().Lookup(name)
		if flag == nil {
			t.Errorf("flag --%s 未注册", name)
			continue
		}
		if flag.DefValue != wantDefault {
			t.Errorf("flag --%s 缺省值 = %q, want %q", name, flag.DefValue, wantDefault)
		}
	}
}

func TestAgentRequiresToken(t *testing.T) {
	t.Setenv("MAKE_AGENT_TOKEN", "")
	previous := agentToken
	agentToken = ""
	t.Cleanup(func() { agentToken = previous })
	err := agentCmd.RunE(agentCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "MAKE_AGENT_TOKEN") {
		t.Errorf("err = %v, want 缺 token 引导", err)
	}
}
