/**
 * [INPUT]: 依赖 cmd 包内的 resolveEnvironment / 全局 Environment（白盒），internal/config（SetSetting）、testing
 * [OUTPUT]: 覆盖环境解析优先级（flag > settings > 默认）与 withGateway 网关前缀拼接的单元测试
 * [POS]: cmd 模块 client.go resolveEnvironment / withGateway 的配套测试，t.Setenv 隔离配置
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/qfeius/makecli/internal/config"
)

// setEnvFlag 临时覆盖全局 Environment（--env），结束自动还原。
func setEnvFlag(t *testing.T, name string) {
	t.Helper()
	old := Environment
	Environment = name
	t.Cleanup(func() { Environment = old })
}

func TestResolveEnvironment(t *testing.T) {
	t.Run("default production when nothing set", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		setEnvFlag(t, "")
		env, err := resolveEnvironment()
		if err != nil {
			t.Fatalf("resolveEnvironment: %v", err)
		}
		if env.MetaServerURL != "https://make.qfei.cn" {
			t.Errorf("default MetaServerURL = %q", env.MetaServerURL)
		}
	})

	t.Run("settings environment over default", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		setEnvFlag(t, "")
		if err := config.SetSetting("environment", "test"); err != nil {
			t.Fatal(err)
		}
		env, err := resolveEnvironment()
		if err != nil {
			t.Fatalf("resolveEnvironment: %v", err)
		}
		if env.RepoServerURL != "https://test-make-repo.qtech.cn" {
			t.Errorf("RepoServerURL = %q, want test", env.RepoServerURL)
		}
	})

	t.Run("--env flag over settings", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		if err := config.SetSetting("environment", "test"); err != nil {
			t.Fatal(err)
		}
		setEnvFlag(t, "production")
		env, err := resolveEnvironment()
		if err != nil {
			t.Fatalf("resolveEnvironment: %v", err)
		}
		if env.MetaServerURL != "https://make.qfei.cn" {
			t.Errorf("MetaServerURL = %q, want production", env.MetaServerURL)
		}
	})

	t.Run("unknown environment errors", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		setEnvFlag(t, "staging")
		if _, err := resolveEnvironment(); err == nil {
			t.Error("expected error for unknown environment")
		}
	})
}

// TestWithGateway 锁定网关前缀拼接的幂等契约：主机基址补 /api/make，
// 已含前缀/尾随斜杠/空串各自的归一化行为。
func TestWithGateway(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://test-make.qtech.cn", "https://test-make.qtech.cn/api/make"},          // 纯主机名补前缀
		{"https://test-make.qtech.cn/", "https://test-make.qtech.cn/api/make"},         // 尾随斜杠先裁后补
		{"https://test-make.qtech.cn/api/make", "https://test-make.qtech.cn/api/make"}, // 幂等：已含前缀原样返回
		{"https://x/api/make/", "https://x/api/make"},                                  // 幂等 + 裁尾斜杠
		{"", ""}, // 空串原样返回
	}
	for _, c := range cases {
		if got := withGateway(c.in); got != c.want {
			t.Errorf("withGateway(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestResolveChannel(t *testing.T) {
	writeSettings := func(t *testing.T, content string) {
		t.Helper()
		dir := t.TempDir()
		t.Setenv(config.EnvConfigDir, dir)
		if content != "" {
			if err := os.WriteFile(filepath.Join(dir, "config"), []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}

	t.Run("unset falls back to stable", func(t *testing.T) {
		writeSettings(t, "")
		ch, err := resolveChannel()
		if err != nil {
			t.Fatal(err)
		}
		if ch != config.ChannelStable {
			t.Fatalf("channel = %q, want stable", ch)
		}
	})

	t.Run("beta from settings", func(t *testing.T) {
		writeSettings(t, "[settings]\nchannel = beta\n")
		ch, err := resolveChannel()
		if err != nil {
			t.Fatal(err)
		}
		if ch != config.ChannelBeta {
			t.Fatalf("channel = %q, want beta", ch)
		}
	})

	t.Run("unknown value rejected", func(t *testing.T) {
		writeSettings(t, "[settings]\nchannel = nightly\n")
		if _, err := resolveChannel(); err == nil {
			t.Fatal("expected error for unknown channel")
		}
	})
}
