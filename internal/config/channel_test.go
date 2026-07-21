/**
 * [INPUT]: 依赖 channel.go 的通道常量与 ChannelNames、settings.go 的 LoadSettings、paths.go 的 EnvConfigDir
 * [OUTPUT]: 通道常量与 [settings] channel 读取的单元测试
 * [POS]: internal/config 的测试，守护发布通道域取值与 Settings.Channel 三态语义
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package config

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestChannelNames(t *testing.T) {
	if names := ChannelNames(); !slices.Equal(names, []string{"stable", "beta"}) {
		t.Fatalf("ChannelNames() = %v, want [stable beta]", names)
	}
	if DefaultChannel != ChannelStable {
		t.Fatalf("DefaultChannel = %q, want %q", DefaultChannel, ChannelStable)
	}
}

func TestLoadSettingsChannel(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvConfigDir, dir)
	content := "[settings]\nchannel = beta\n"
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if s.Channel != ChannelBeta {
		t.Fatalf("Channel = %q, want %q", s.Channel, ChannelBeta)
	}
}

func TestLoadSettingsChannelUnset(t *testing.T) {
	t.Setenv(EnvConfigDir, t.TempDir())
	s, err := LoadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if s.Channel != "" {
		t.Fatalf("Channel = %q, want empty (未配置)", s.Channel)
	}
}
