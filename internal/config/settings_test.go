/**
 * [INPUT]: 依赖 config 包内 LoadSettings / LoadConfig / ConfigPath / settingsSection（白盒）
 * [OUTPUT]: 覆盖 [settings] 全局段读取与 profile 解析隔离的单元测试
 * [POS]: internal/config 模块 settings.go 的配套测试
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// writeConfigFile 在 ConfigPath 处写入 config 文件内容
func writeConfigFile(t *testing.T, content string) {
	t.Helper()
	path, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestLoadSettings_NoFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if s.CheckForUpdates != nil {
		t.Errorf("expected nil CheckForUpdates, got %v", *s.CheckForUpdates)
	}
}

func TestLoadSettings_Disabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigFile(t, "[settings]\ncheck-for-updates = false\n\n[default]\nX-Tenant-ID = t1\n")

	s, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if s.CheckForUpdates == nil {
		t.Fatal("expected CheckForUpdates set, got nil")
	}
	if *s.CheckForUpdates {
		t.Errorf("CheckForUpdates = %v, want false", *s.CheckForUpdates)
	}
}

func TestLoadSettings_Enabled(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigFile(t, "[settings]\ncheck-for-updates = true\n")
	s, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if s.CheckForUpdates == nil || *s.CheckForUpdates != true {
		t.Errorf("expected true, got %v", s.CheckForUpdates)
	}
}

func TestLoadConfig_IgnoresSettingsSection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigFile(t, "[settings]\ncheck-for-updates = false\n\n[default]\nX-Tenant-ID = t1\n")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if _, ok := cfg[settingsSection]; ok {
		t.Error("settings section should not appear as a profile")
	}
	if cfg["default"].XTenantID != "t1" {
		t.Errorf("default profile lost: %+v", cfg["default"])
	}
}

func TestSaveConfig_PreservesSettings(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigFile(t, "[settings]\ncheck-for-updates = false\n\n[default]\nX-Tenant-ID = t1\n")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	s, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if s.CheckForUpdates == nil || *s.CheckForUpdates != false {
		t.Errorf("settings lost across SaveConfig round-trip: %v", s.CheckForUpdates)
	}
}

func TestLoadSettings_Environment(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	writeConfigFile(t, "[settings]\nenvironment = test\n")
	s, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if s.Environment != "test" {
		t.Errorf("Environment = %q, want test", s.Environment)
	}
}

func TestSetSetting_WritesAndPreserves(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// 预置：一个 profile + 一个已有 settings 键
	writeConfigFile(t, "[settings]\ncheck-for-updates = false\n\n[default]\nserver-url = https://x/api/make\n")

	if err := SetSetting("environment", "production"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}

	s, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if s.Environment != "production" {
		t.Errorf("Environment = %q, want production", s.Environment)
	}
	// 既有 settings 键保留
	if s.CheckForUpdates == nil || *s.CheckForUpdates != false {
		t.Errorf("check-for-updates lost across SetSetting: %v", s.CheckForUpdates)
	}
	// profile 段保留
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg["default"].ServerURL != "https://x/api/make" {
		t.Errorf("profile lost across SetSetting: %+v", cfg["default"])
	}
}

func TestSetSetting_NoExistingFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := SetSetting("environment", "test"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	s, err := LoadSettings()
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if s.Environment != "test" {
		t.Errorf("Environment = %q, want test", s.Environment)
	}
}

func TestValidateProfileName(t *testing.T) {
	if err := ValidateProfileName("settings"); err == nil {
		t.Error("'settings' must be rejected as a profile name (reserved section)")
	}
	for _, name := range []string{"default", "test", "production", "my-profile"} {
		if err := ValidateProfileName(name); err != nil {
			t.Errorf("%q should be a valid profile name: %v", name, err)
		}
	}
}

func TestSaveRejectsReservedProfile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := SaveConfig(Config{"settings": {ServerURL: "https://x"}}); err == nil {
		t.Error("SaveConfig should reject a profile named 'settings'")
	}
	if err := Save(Credentials{"settings": {AccessToken: "tok"}}); err == nil {
		t.Error("Save (credentials) should reject a profile named 'settings'")
	}
}
