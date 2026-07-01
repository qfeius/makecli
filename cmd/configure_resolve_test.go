/**
 * [INPUT]: 依赖 encoding/json、strings、testing、internal/config；对白盒 runConfigureResolve / outputJSON / outputTable 断言
 * [OUTPUT]: 覆盖 configure resolve 的 local-preview JSON 合约、环境解析优先级、profile/flag override 与 origin 归一化
 * [POS]: cmd 模块 configure resolve 子命令的配套测试
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/qfeius/makecli/internal/config"
)

func TestRunConfigureResolveLocalPreview(t *testing.T) {
	t.Run("default production", func(t *testing.T) {
		t.Setenv(config.EnvConfigDir, t.TempDir())
		resetConfigureResolveGlobals(t)

		result, out := runConfigureResolveForTest(t, "local-preview", outputJSON)

		if result.Profile != "default" {
			t.Errorf("profile = %q, want default", result.Profile)
		}
		if result.Environment != "production" {
			t.Errorf("environment = %q, want production", result.Environment)
		}
		if result.MakeAPIOrigin != "https://make.qtech.cn" {
			t.Errorf("make_api_origin = %q, want production origin", result.MakeAPIOrigin)
		}
		if result.TenantID != "" || result.OperatorID != "" {
			t.Errorf("tenant/operator should be empty by default, got %q/%q", result.TenantID, result.OperatorID)
		}
		if !strings.Contains(out, "\"make_api_origin\"") {
			t.Errorf("json output missing make_api_origin: %s", out)
		}
	})

	t.Run("settings environment", func(t *testing.T) {
		t.Setenv(config.EnvConfigDir, t.TempDir())
		resetConfigureResolveGlobals(t)
		if err := config.SetSetting("environment", "test"); err != nil {
			t.Fatal(err)
		}

		result, _ := runConfigureResolveForTest(t, "local-preview", outputJSON)

		if result.Environment != "test" {
			t.Errorf("environment = %q, want test", result.Environment)
		}
		if result.MakeAPIOrigin != "https://test-make.qtech.cn" {
			t.Errorf("make_api_origin = %q, want test origin", result.MakeAPIOrigin)
		}
	})

	t.Run("env flag overrides settings", func(t *testing.T) {
		t.Setenv(config.EnvConfigDir, t.TempDir())
		resetConfigureResolveGlobals(t)
		setEnvironmentFlag(t, "dev")
		if err := config.SetSetting("environment", "test"); err != nil {
			t.Fatal(err)
		}

		result, _ := runConfigureResolveForTest(t, "local-preview", outputJSON)

		if result.Environment != "dev" {
			t.Errorf("environment = %q, want dev", result.Environment)
		}
		if result.MakeAPIOrigin != "https://dev-make.qtech.cn" {
			t.Errorf("make_api_origin = %q, want dev origin", result.MakeAPIOrigin)
		}
	})

	t.Run("profile override and headers", func(t *testing.T) {
		t.Setenv(config.EnvConfigDir, t.TempDir())
		resetConfigureResolveGlobals(t)
		if err := config.SaveConfig(config.Config{
			"default": {
				MetaServerURL: "https://custom-make.example.com",
				XTenantID:     "tenant-1",
				OperatorID:    "operator-1",
			},
		}); err != nil {
			t.Fatal(err)
		}

		result, _ := runConfigureResolveForTest(t, "local-preview", outputJSON)

		if result.MakeAPIOrigin != "https://custom-make.example.com" {
			t.Errorf("make_api_origin = %q, want custom origin", result.MakeAPIOrigin)
		}
		if result.TenantID != "tenant-1" || result.OperatorID != "operator-1" {
			t.Errorf("tenant/operator = %q/%q, want tenant-1/operator-1", result.TenantID, result.OperatorID)
		}
	})

	t.Run("meta flag overrides profile", func(t *testing.T) {
		t.Setenv(config.EnvConfigDir, t.TempDir())
		resetConfigureResolveGlobals(t)
		setMetaServerURL(t, "https://flag-make.example.com")
		if err := config.SaveConfig(config.Config{
			"default": {MetaServerURL: "https://profile-make.example.com"},
		}); err != nil {
			t.Fatal(err)
		}

		result, _ := runConfigureResolveForTest(t, "local-preview", outputJSON)

		if result.MakeAPIOrigin != "https://flag-make.example.com" {
			t.Errorf("make_api_origin = %q, want flag origin", result.MakeAPIOrigin)
		}
	})

	t.Run("strips gateway path", func(t *testing.T) {
		t.Setenv(config.EnvConfigDir, t.TempDir())
		resetConfigureResolveGlobals(t)
		if err := config.SaveConfig(config.Config{
			"default": {MetaServerURL: "https://test-make.qtech.cn/api/make/"},
		}); err != nil {
			t.Fatal(err)
		}

		result, _ := runConfigureResolveForTest(t, "local-preview", outputJSON)

		if result.MakeAPIOrigin != "https://test-make.qtech.cn" {
			t.Errorf("make_api_origin = %q, want bare origin", result.MakeAPIOrigin)
		}
	})
}

func TestRunConfigureResolveRejectsUnsupportedOptions(t *testing.T) {
	t.Setenv(config.EnvConfigDir, t.TempDir())
	resetConfigureResolveGlobals(t)

	if _, err := runConfigureResolve("repo", outputJSON); err == nil {
		t.Fatal("expected unsupported target error")
	}
	if _, err := runConfigureResolve("local-preview", outputTable); err == nil {
		t.Fatal("expected unsupported output error")
	}
}

func runConfigureResolveForTest(t *testing.T, target, output string) (*configureResolveResult, string) {
	t.Helper()

	var result *configureResolveResult
	out := captureStdout(t, func() {
		var err error
		result, err = runConfigureResolve(target, output)
		if err != nil {
			t.Fatalf("runConfigureResolve: %v", err)
		}
	})

	var decoded configureResolveResult
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("json output invalid: %v\n%s", err, out)
	}
	if *result != decoded {
		t.Fatalf("returned result %#v does not match json %#v", result, decoded)
	}
	return result, out
}

func resetConfigureResolveGlobals(t *testing.T) {
	t.Helper()
	oldProfile := Profile
	oldEnvironment := Environment
	oldMetaServerURL := MetaServerURL
	Profile = "default"
	Environment = ""
	MetaServerURL = ""
	t.Cleanup(func() {
		Profile = oldProfile
		Environment = oldEnvironment
		MetaServerURL = oldMetaServerURL
	})
}

func setEnvironmentFlag(t *testing.T, name string) {
	t.Helper()
	old := Environment
	Environment = name
	t.Cleanup(func() { Environment = old })
}

func setMetaServerURL(t *testing.T, url string) {
	t.Helper()
	old := MetaServerURL
	MetaServerURL = url
	t.Cleanup(func() { MetaServerURL = old })
}
