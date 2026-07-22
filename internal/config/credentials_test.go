/**
 * [INPUT]: 依赖 config 包内的 parseINI、Load、Save、CredentialsPath（包内白盒）
 * [OUTPUT]: 覆盖 INI 解析与凭证读写全路径的单元测试（含 INI 注入拒绝）
 * [POS]: internal/config 模块 credentials.go 的配套测试
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package config

import (
	"os"
	"path/filepath"
	"testing"
)

// ---------------------------------- parseINI ----------------------------------

// writeTempINI 将 INI 内容写入临时文件并将读指针归零，返回可直接使用的 *os.File
func writeTempINI(t *testing.T, content string) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "credentials-*.ini")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestParseINI(t *testing.T) {
	t.Run("empty file", func(t *testing.T) {
		f := writeTempINI(t, "")
		defer func() { _ = f.Close() }()

		creds, err := parseINI(f)
		if err != nil {
			t.Fatalf("parseINI: %v", err)
		}
		if len(creds) != 0 {
			t.Errorf("expected empty credentials, got %v", creds)
		}
	})

	t.Run("single profile", func(t *testing.T) {
		f := writeTempINI(t, "[default]\naccess_token = mytoken\n")
		defer func() { _ = f.Close() }()

		creds, err := parseINI(f)
		if err != nil {
			t.Fatalf("parseINI: %v", err)
		}
		if got := creds["default"].AccessToken; got != "mytoken" {
			t.Errorf("AccessToken = %q, want %q", got, "mytoken")
		}
	})

	t.Run("multiple profiles", func(t *testing.T) {
		content := "[default]\naccess_token = token1\n\n[work]\naccess_token = token2\n"
		f := writeTempINI(t, content)
		defer func() { _ = f.Close() }()

		creds, err := parseINI(f)
		if err != nil {
			t.Fatalf("parseINI: %v", err)
		}
		if got := creds["default"].AccessToken; got != "token1" {
			t.Errorf("default AccessToken = %q, want %q", got, "token1")
		}
		if got := creds["work"].AccessToken; got != "token2" {
			t.Errorf("work AccessToken = %q, want %q", got, "token2")
		}
	})

	t.Run("skips comments and blank lines", func(t *testing.T) {
		content := "# top comment\n\n[default]\n; inline comment\naccess_token = tok\n"
		f := writeTempINI(t, content)
		defer func() { _ = f.Close() }()

		creds, err := parseINI(f)
		if err != nil {
			t.Fatalf("parseINI: %v", err)
		}
		if got := creds["default"].AccessToken; got != "tok" {
			t.Errorf("AccessToken = %q, want %q", got, "tok")
		}
	})

	t.Run("ignores keys outside any section", func(t *testing.T) {
		content := "access_token = orphan\n[default]\naccess_token = real\n"
		f := writeTempINI(t, content)
		defer func() { _ = f.Close() }()

		creds, err := parseINI(f)
		if err != nil {
			t.Fatalf("parseINI: %v", err)
		}
		if got := creds["default"].AccessToken; got != "real" {
			t.Errorf("AccessToken = %q, want %q", got, "real")
		}
	})
}

// ---------------------------------- Load / Save ----------------------------------

func TestLoadNonExistent(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	creds, err := Load()
	if err != nil {
		t.Fatalf("Load on nonexistent file returned error: %v", err)
	}
	if len(creds) != 0 {
		t.Errorf("expected empty credentials, got %v", creds)
	}
}

func TestSaveAndLoad(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	original := Credentials{
		"default": {AccessToken: "token-default"},
		"work":    {AccessToken: "token-work"},
	}

	if err := Save(original); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// 验证文件权限为 0600
	path, _ := CredentialsPath()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("credentials file not found: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("file permissions = %v, want 0600", info.Mode().Perm())
	}

	// 验证目录权限为 0700
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("~/.make dir not found: %v", err)
	}
	if dirInfo.Mode().Perm() != 0700 {
		t.Errorf("dir permissions = %v, want 0700", dirInfo.Mode().Perm())
	}

	// 读回并逐 profile 对比
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for profile, want := range original {
		if got := loaded[profile].AccessToken; got != want.AccessToken {
			t.Errorf("profile %q: AccessToken = %q, want %q", profile, got, want.AccessToken)
		}
	}
}

func TestSaveDefaultFirst(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	creds := Credentials{
		"zzz":     {AccessToken: "last"},
		"default": {AccessToken: "first"},
	}
	if err := Save(creds); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// 读回文件原始内容，[default] 必须先出现
	path, _ := CredentialsPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(raw)
	defaultIdx := indexOf(content, "[default]")
	zzzIdx := indexOf(content, "[zzz]")
	if defaultIdx > zzzIdx {
		t.Errorf("[default] should appear before [zzz] in file, got:\n%s", content)
	}
}

// TestSaveRejectsInjection 锁定 INI 注入防线：section 注入形的 profile 名与含换行/首尾空白的
// token 值都必须在落盘前被拒绝，且不留下文件。
func TestSaveRejectsInjection(t *testing.T) {
	t.Run("profile name with section injection", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		err := Save(Credentials{"evil]\n[other": {AccessToken: "tok"}})
		if err == nil {
			t.Fatal("Save must reject a profile name containing INI syntax")
		}
		path, _ := CredentialsPath()
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Error("rejected Save must not leave a credentials file behind")
		}
	})

	t.Run("token with newline", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		err := Save(Credentials{"default": {AccessToken: "tok\n[evil]\naccess_token = stolen"}})
		if err == nil {
			t.Fatal("Save must reject a token containing a newline")
		}
	})

	t.Run("token with surrounding whitespace", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		if err := Save(Credentials{"default": {AccessToken: " tok "}}); err == nil {
			t.Fatal("Save must reject a token with leading/trailing whitespace")
		}
	})
}

func indexOf(s, sub string) int {
	for i := range s {
		if len(s)-i >= len(sub) && s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
