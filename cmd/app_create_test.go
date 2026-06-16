/**
 * [INPUT]: 依赖 cmd 包内的 runAppCreate/runAppCreateFromFile（包内白盒），internal/config、encoding/json、net/http、net/http/httptest、os、path/filepath
 * [OUTPUT]: 覆盖 app create 子命令核心逻辑的单元测试（含 -f 文件模式）
 * [POS]: cmd 模块 app_create.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/qfeius/makecli/internal/config"
)

func TestRunAppCreate(t *testing.T) {
	t.Run("creates app via API", func(t *testing.T) {
		srv := newMockMeta(t, 200, "create app success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL
		stubRepoServer(t, srv.URL)

		if err := runAppCreate("myapp", "", ""); err != nil {
			t.Fatalf("runAppCreate: %v", err)
		}
	})

	t.Run("prints code repositories after create", func(t *testing.T) {
		srv := newMockMeta(t, 200, "create app success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL
		stubRepoServer(t, newMockRepoServer(t).URL)

		out := captureStdout(t, func() {
			if err := runAppCreate("myapp", "", ""); err != nil {
				t.Errorf("runAppCreate: %v", err)
			}
		})
		if !strings.Contains(out, "myapp-preview.git") || !strings.Contains(out, "myapp-production.git") {
			t.Errorf("output missing clone urls: %q", out)
		}
	})

	t.Run("repo preparation failure does not fail create", func(t *testing.T) {
		srv := newMockMeta(t, 200, "create app success")
		defer srv.Close()
		repoSrv := newMockMeta(t, 500, "repository could not be prepared")
		defer repoSrv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL
		stubRepoServer(t, repoSrv.URL)

		if err := runAppCreate("myapp", "", ""); err != nil {
			t.Fatalf("repo failure should not fail app create: %v", err)
		}
	})

	t.Run("uses --name as key when key omitted", func(t *testing.T) {
		srv := newMockMeta(t, 200, "create app success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL
		stubRepoServer(t, srv.URL)

		cmd := newAppCreateCmd()
		cmd.SetArgs([]string{"--name", "myapp"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("app create --name myapp: %v", err)
		}
	})

	t.Run("fails without key and name", func(t *testing.T) {
		cmd := newAppCreateCmd()
		cmd.SetArgs([]string{})
		cmd.SilenceErrors = true
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected error without key, --name and -f")
		}
	})

	t.Run("rejects invalid app name", func(t *testing.T) {
		cases := []struct {
			name string
			desc string
		}{
			{"my-app", "contains hyphen"},
			{"my app", "contains space"},
			{"my.app", "contains dot"},
			{"我的app", "contains chinese"},
			{"a_very_long_name_that_is", "exceeds 20 chars"},
			{"", "empty string"},
		}
		for _, tc := range cases {
			if err := runAppCreate(tc.name, "", ""); err == nil {
				t.Errorf("expected error for %s (%s)", tc.name, tc.desc)
			}
		}
	})

	t.Run("creates app with description", func(t *testing.T) {
		srv := newMockMeta(t, 200, "create app success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL
		stubRepoServer(t, srv.URL)

		if err := runAppCreate("myapp", "test app", ""); err != nil {
			t.Fatalf("runAppCreate with description: %v", err)
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		MetaServerURL = "http://unused"
		// 未写入任何凭证，预期报错
		if err := runAppCreate("myapp", "", ""); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails on API error response", func(t *testing.T) {
		srv := newMockMeta(t, 400, "invalid app name")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		if err := runAppCreate("myapp", "", ""); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = "http://unused"
		setProfile(t, "nonexistent")

		if err := runAppCreate("myapp", "", ""); err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})
}

func TestRunAppCreateFromFile(t *testing.T) {
	t.Run("creates app from YAML file", func(t *testing.T) {
		srv := newMockMeta(t, 200, "create app success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL
		stubRepoServer(t, srv.URL)

		f := filepath.Join(t.TempDir(), "app.yaml")
		writeTestFile(t, f, []byte("key: fileapp\nname: 文件应用\ntype: Make.App\nproperties:\n  description: from file\n"))

		if err := runAppCreateFromFile(f); err != nil {
			t.Fatalf("runAppCreateFromFile: %v", err)
		}
	})

	t.Run("creates app from YAML file without properties", func(t *testing.T) {
		srv := newMockMeta(t, 200, "create app success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL
		stubRepoServer(t, srv.URL)

		f := filepath.Join(t.TempDir(), "app.yml")
		writeTestFile(t, f, []byte("key: bareapp\nname: 简易应用\ntype: Make.App\n"))

		if err := runAppCreateFromFile(f); err != nil {
			t.Fatalf("runAppCreateFromFile without props: %v", err)
		}
	})

	t.Run("fails on non-yaml file", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "app.json")
		writeTestFile(t, f, []byte(`{}`))

		if err := runAppCreateFromFile(f); err == nil {
			t.Fatal("expected error for non-yaml file")
		}
	})

	t.Run("fails when no Make.App in file", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "entity.yaml")
		writeTestFile(t, f, []byte("key: foo\nname: 测试\ntype: Make.Entity\nappKey: bar\n"))

		if err := runAppCreateFromFile(f); err == nil {
			t.Fatal("expected error for missing Make.App")
		}
	})

	t.Run("fails when multiple Make.App in file", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "multi.yaml")
		writeTestFile(t, f, []byte("key: appone\nname: 一号\ntype: Make.App\n---\nkey: apptwo\nname: 二号\ntype: Make.App\n"))

		if err := runAppCreateFromFile(f); err == nil {
			t.Fatal("expected error for multiple Make.App")
		}
	})
}

func TestValidResourceKey(t *testing.T) {
	valid := []string{"ab", "abc", "MyApp", "app_01", "A1_b2_C3", "12345678901234567890"}
	for _, key := range valid {
		if err := validResourceKey(key); err != nil {
			t.Errorf("validResourceKey(%q) unexpected error: %v", key, err)
		}
	}

	invalid := []string{"", "a", "_underscore", "my-app", "my app", "my.app", "我的app", "a_very_long_name_that_is", "app@home"}
	for _, key := range invalid {
		if err := validResourceKey(key); err == nil {
			t.Errorf("validResourceKey(%q) expected error, got nil", key)
		}
	}
}

// writeTestFile 在指定路径写入测试文件，失败则终止测试
func writeTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

// newMockMeta 启动一个返回固定 code/message 的测试 Meta Server
func newMockMeta(t *testing.T, code int, message string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":    code,
			"message": message,
			"data":    map[string]any{},
		})
	}))
}

// stubRepoServer 测试期间把代码仓库服务指向给定 URL，结束自动还原
func stubRepoServer(t *testing.T, url string) {
	t.Helper()
	old := RepoServerURL
	RepoServerURL = url
	t.Cleanup(func() { RepoServerURL = old })
}

// saveDefaultToken 在当前 HOME 下写入 default profile 的测试 JWT
func saveDefaultToken(t *testing.T) {
	t.Helper()
	// 合法 JWT 格式（三段 base64url），validateJWT 校验通过
	fakeToken := "eyJ0eXAiOiJKV1QifQ.eyJzdWIiOiJ0ZXN0In0.c2lnbmF0dXJl"
	if err := config.Save(config.Credentials{
		"default": config.Profile{AccessToken: fakeToken},
	}); err != nil {
		t.Fatal(err)
	}
}
