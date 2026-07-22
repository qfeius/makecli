/**
 * [INPUT]: 依赖 daemon.go 的 resolveAgentGatewayURL；setEnvFlag（client_test.go）临时覆盖全局 Environment
 * [OUTPUT]: 对外提供 gateway 地址取值链的单元测试——flag > env var > 环境 preset
 * [POS]: cmd 模块的 daemon 测试面——锁定"缺省零配置连对环境"的解析纪律
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import "testing"

func TestResolveAgentGatewayURLPresetByEnvironment(t *testing.T) {
	t.Setenv("MAKE_CLI_CONFIG_DIR", t.TempDir()) // 隔离 [settings] environment
	tests := []struct {
		environment string
		want        string
	}{
		{"dev", "https://dev-make-agent.qtech.cn"},
		{"test", "https://test-make-agent.qtech.cn"},
		{"production", "https://make-agent.qfei.cn"},
	}
	for _, tt := range tests {
		setEnvFlag(t, tt.environment)
		url, err := resolveAgentGatewayURL()
		if err != nil {
			t.Fatalf("resolve(%s): %v", tt.environment, err)
		}
		if url != tt.want {
			t.Fatalf("resolve(%s) = %q, want %q", tt.environment, url, tt.want)
		}
	}
}

func TestResolveAgentGatewayURLEnvVarOverridesPreset(t *testing.T) {
	t.Setenv("MAKE_CLI_CONFIG_DIR", t.TempDir())
	t.Setenv("MAKE_AGENT_GATEWAY_URL", "http://10.26.2.221:8081")
	setEnvFlag(t, "production")
	url, err := resolveAgentGatewayURL()
	if err != nil {
		t.Fatal(err)
	}
	if url != "http://10.26.2.221:8081" {
		t.Fatalf("env var 应覆盖 preset: %q", url)
	}
}

func TestResolveAgentGatewayURLFlagWins(t *testing.T) {
	t.Setenv("MAKE_CLI_CONFIG_DIR", t.TempDir())
	t.Setenv("MAKE_AGENT_GATEWAY_URL", "http://from-env:1")
	original := daemonGatewayURL
	daemonGatewayURL = "http://from-flag:2"
	t.Cleanup(func() { daemonGatewayURL = original })
	url, err := resolveAgentGatewayURL()
	if err != nil {
		t.Fatal(err)
	}
	if url != "http://from-flag:2" {
		t.Fatalf("flag 应最高优先: %q", url)
	}
}
