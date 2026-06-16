/**
 * [INPUT]: 依赖 cmd 包内的 resolveEnvironment / 全局 Environment（白盒），internal/config（SetSetting）、testing
 * [OUTPUT]: 覆盖环境解析优先级（flag > settings > 默认）的单元测试
 * [POS]: cmd 模块 client.go resolveEnvironment 的配套测试，t.Setenv 隔离配置
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
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
	t.Run("default dev when nothing set", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		setEnvFlag(t, "")
		env, err := resolveEnvironment()
		if err != nil {
			t.Fatalf("resolveEnvironment: %v", err)
		}
		if env.MetaServerURL != "https://dev-make.qtech.cn/api/make" {
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
		if env.RepoServerURL != "https://test-make-repo.qtech.cn/api/make" {
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
		if env.MetaServerURL != "https://make.qtech.cn/api/make" {
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
