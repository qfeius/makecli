/**
 * [INPUT]: 依赖 config 包内的 parseConfigINI、LoadConfig、SaveConfig、ConfigPath（包内白盒）
 * [OUTPUT]: 覆盖 INI 解析与 config 读写全路径的单元测试
 * [POS]: internal/config 模块 config.go 的配套测试
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------- Config ----------------------------------

func TestConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := ConfigPath()
	if err != nil {
		t.Fatalf("ConfigPath: %v", err)
	}
	want := filepath.Join(home, ".make", "config")
	if path != want {
		t.Errorf("ConfigPath = %q, want %q", path, want)
	}
}

func TestLoadConfigNonExistent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if len(cfg) != 0 {
		t.Errorf("expected empty config, got %v", cfg)
	}
}

func TestSaveConfigAndLoad(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	original := Config{
		"default": {XTenantID: "tenant-1", OperatorID: "op-1"},
		"staging": {XTenantID: "tenant-2", OperatorID: ""},
	}

	if err := SaveConfig(original); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	path, _ := ConfigPath()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("config file not found: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("file permissions = %v, want 0600", info.Mode().Perm())
	}

	loaded, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	for profile, want := range original {
		got := loaded[profile]
		if got.XTenantID != want.XTenantID {
			t.Errorf("profile %q: XTenantID = %q, want %q", profile, got.XTenantID, want.XTenantID)
		}
		if got.OperatorID != want.OperatorID {
			t.Errorf("profile %q: OperatorID = %q, want %q", profile, got.OperatorID, want.OperatorID)
		}
	}
}

func TestParseConfigINI(t *testing.T) {
	t.Run("both keys", func(t *testing.T) {
		f := writeTempINI(t, "[default]\nX-Tenant-ID = t1\nX-Operator-ID = o1\n")
		defer func() { _ = f.Close() }()

		cfg, err := parseConfigINI(f)
		if err != nil {
			t.Fatalf("parseConfigINI: %v", err)
		}
		if cfg["default"].XTenantID != "t1" || cfg["default"].OperatorID != "o1" {
			t.Errorf("unexpected config: %+v", cfg["default"])
		}
	})

	t.Run("partial keys", func(t *testing.T) {
		f := writeTempINI(t, "[default]\nX-Tenant-ID = t1\n")
		defer func() { _ = f.Close() }()

		cfg, err := parseConfigINI(f)
		if err != nil {
			t.Fatalf("parseConfigINI: %v", err)
		}
		if cfg["default"].XTenantID != "t1" {
			t.Errorf("XTenantID = %q, want %q", cfg["default"].XTenantID, "t1")
		}
		if cfg["default"].OperatorID != "" {
			t.Errorf("OperatorID = %q, want empty", cfg["default"].OperatorID)
		}
	})
}
