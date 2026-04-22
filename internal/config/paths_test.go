/**
 * [INPUT]: 依赖 config 包内的 Dir、CredentialsPath、ConfigPath、EnvConfigDir（包内白盒）
 * [OUTPUT]: 覆盖 $MAKE_CLI_CONFIG_DIR 覆盖语义的单元测试
 * [POS]: internal/config 模块 paths.go 的配套测试
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package config

import (
	"path/filepath"
	"testing"
)

func TestDirDefaultsToHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvConfigDir, "")

	got, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if want := filepath.Join(home, ".make"); got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
}

func TestDirRespectsEnvOverride(t *testing.T) {
	override := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv(EnvConfigDir, override)

	got, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if got != override {
		t.Errorf("Dir = %q, want %q", got, override)
	}

	credPath, _ := CredentialsPath()
	if want := filepath.Join(override, "credentials"); credPath != want {
		t.Errorf("CredentialsPath = %q, want %q", credPath, want)
	}

	cfgPath, _ := ConfigPath()
	if want := filepath.Join(override, "config"); cfgPath != want {
		t.Errorf("ConfigPath = %q, want %q", cfgPath, want)
	}
}

func TestSaveHonorsEnvOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "nested", "make-cfg")
	t.Setenv("HOME", t.TempDir())
	t.Setenv(EnvConfigDir, override)

	if err := Save(Credentials{"default": {AccessToken: "tok"}}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := loaded["default"].AccessToken; got != "tok" {
		t.Errorf("AccessToken = %q, want %q", got, "tok")
	}
}
