/**
 * [INPUT]: 依赖 cmd 包内函数（包内白盒）、internal/config、encoding/json、net/http、net/http/httptest、os、path/filepath、testing
 * [OUTPUT]: 覆盖 apply 子命令核心逻辑的单元测试
 * [POS]: cmd 模块顶层 apply 命令的配套测试，用 httptest 隔离网络、临时文件测试 YAML 解析
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/MakeHQ/makecli/internal/config"
)

// ---------------------------------- apply 测试 ----------------------------------

func TestRunAppApply(t *testing.T) {
	t.Run("applies single app from file", func(t *testing.T) {
		srv := newMockMetaForApply(t, 200, "create app success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultTokenForApply(t)
		testDir := t.TempDir()

		yamlFile := writeYAMLFileForApply(t, testDir, "app.yaml", `name: myapp
type: Make.App
meta:
  version: 1.0.0
properties:
  code: custom_code
`)

		if err := runAppApply(yamlFile, "default", srv.URL); err != nil {
			t.Fatalf("runAppApply: %v", err)
		}
	})

	t.Run("applies multi-document YAML", func(t *testing.T) {
		callCount := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callCount++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"code":200,"msg":"success","data":{}}`))
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultTokenForApply(t)
		testDir := t.TempDir()

		yamlFile := writeYAMLFileForApply(t, testDir, "multi.yaml", `name: app1
type: Make.App
meta:
  version: 1.0.0
properties:
  code: app1
---
name: app2
type: Make.App
meta:
  version: 1.0.0
properties:
  code: app2
`)

		if err := runAppApply(yamlFile, "default", srv.URL); err != nil {
			t.Fatalf("runAppApply multi-doc: %v", err)
		}
		if callCount != 2 {
			t.Fatalf("expected 2 API calls, got %d", callCount)
		}
	})

	t.Run("applies app then entity from directory", func(t *testing.T) {
		callCount := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callCount++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"code":200,"msg":"success","data":{}}`))
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultTokenForApply(t)
		testDir := t.TempDir()

		writeYAMLFileForApply(t, testDir, "app.yaml", `name: myapp
type: Make.App
meta:
  version: 1.0.0
properties:
  code: myapp
`)
		writeYAMLFileForApply(t, testDir, "entity.yaml", `name: Task
type: Make.Entity
app: myapp
meta:
  version: 1.0.0
properties:
  fields:
    - name: title
      type: Make.Field.Text
      meta:
        version: 1.0.0
      properties: {}
`)

		if err := runAppApply(testDir, "default", srv.URL); err != nil {
			t.Fatalf("runAppApply dir: %v", err)
		}
		if callCount != 2 {
			t.Fatalf("expected 2 API calls, got %d", callCount)
		}
	})

	t.Run("fails with missing credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		testDir := t.TempDir()
		// 不写入凭证，测试缺失凭证的情况

		yamlFile := writeYAMLFileForApply(t, testDir, "app.yaml", `name: test
type: Make.App
meta:
  version: 1.0.0
properties:
  code: test
`)

		if err := runAppApply(yamlFile, "default", "http://localhost"); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		testDir := t.TempDir()
		saveDefaultTokenForApply(t)
		yamlFile := writeYAMLFileForApply(t, testDir, "app.yaml", `name: test
type: Make.App
meta:
  version: 1.0.0
properties:
  code: test
`)

		if err := runAppApply(yamlFile, "unknown", "http://localhost"); err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})

	t.Run("fails on API error", func(t *testing.T) {
		srv := newMockMetaForApply(t, 400, "invalid app")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultTokenForApply(t)
		testDir := t.TempDir()

		yamlFile := writeYAMLFileForApply(t, testDir, "app.yaml", `name: test
type: Make.App
meta:
  version: 1.0.0
properties:
  code: test
`)

		if err := runAppApply(yamlFile, "default", srv.URL); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails with entity missing app", func(t *testing.T) {
		srv := newMockMetaForApply(t, 200, "create entity success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultTokenForApply(t)
		testDir := t.TempDir()

		yamlFile := writeYAMLFileForApply(t, testDir, "entity.yaml", `name: Task
type: Make.Entity
meta:
  version: 1.0.0
properties:
  fields: []
`)

		if err := runAppApply(yamlFile, "default", srv.URL); err == nil {
			t.Fatal("expected error for missing app field")
		}
	})

	t.Run("fails on invalid YAML", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultTokenForApply(t)
		testDir := t.TempDir()
		bad := filepath.Join(testDir, "bad.yaml")
		os.WriteFile(bad, []byte("invalid: yaml: ["), 0644)

		if err := runAppApply(bad, "default", "http://localhost"); err == nil {
			t.Fatal("expected error for invalid YAML")
		}
	})
}

func TestRunAppApplyFailsWithoutRecognizedYAMLFiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	saveDefaultTokenForApply(t)
	testDir := t.TempDir()
	writeYAMLFileForApply(t, testDir, "app.json", `{"name":"app1"}`)

	err := runAppApply(testDir, "default", "http://localhost")
	if err == nil {
		t.Fatal("expected error for directory without yaml files")
	}

	want := "error reading [" + testDir + "]: recognized file extensions are [.yaml .yml]"
	if err.Error() != want {
		t.Fatalf("expected %q, got %q", want, err.Error())
	}
}

// ---------------------------------- loadManifestsFromFile 测试 ----------------------------------

func TestLoadManifestsFromFile(t *testing.T) {
	t.Run("loads single document", func(t *testing.T) {
		data := `name: myapp
type: Make.App
meta:
  version: 1.0.0
properties:
  code: myapp
`
		testDir := t.TempDir()
		file := writeYAMLFileForApply(t, testDir, "test.yaml", data)
		manifests, err := loadManifestsFromFile(file)
		if err != nil {
			t.Fatalf("loadManifestsFromFile: %v", err)
		}
		if len(manifests) != 1 {
			t.Fatalf("expected 1 manifest, got %d", len(manifests))
		}
		if manifests[0].Name != "myapp" {
			t.Errorf("expected name myapp, got %s", manifests[0].Name)
		}
	})

	t.Run("loads multi-document", func(t *testing.T) {
		data := `name: app1
type: Make.App
meta:
  version: 1.0.0
properties:
  code: app1
---
name: app2
type: Make.App
meta:
  version: 1.0.0
properties:
  code: app2
`
		testDir := t.TempDir()
		file := writeYAMLFileForApply(t, testDir, "test.yaml", data)
		manifests, err := loadManifestsFromFile(file)
		if err != nil {
			t.Fatalf("loadManifestsFromFile: %v", err)
		}
		if len(manifests) != 2 {
			t.Fatalf("expected 2 manifests, got %d", len(manifests))
		}
	})

	t.Run("skips documents with missing required fields", func(t *testing.T) {
		data := `meta:
  version: 1.0.0
---
name: app2
type: Make.App
meta:
  version: 1.0.0
properties:
  code: app2
`
		testDir := t.TempDir()
		file := writeYAMLFileForApply(t, testDir, "test.yaml", data)
		manifests, err := loadManifestsFromFile(file)
		if err != nil {
			t.Fatalf("loadManifestsFromFile: %v", err)
		}
		if len(manifests) != 1 {
			t.Fatalf("expected 1 manifest (one skipped), got %d", len(manifests))
		}
		if manifests[0].Name != "app2" {
			t.Errorf("expected app2, got %s", manifests[0].Name)
		}
	})
}

// ---------------------------------- loadManifestsFromDir 测试 ----------------------------------

func TestLoadManifestsFromDir(t *testing.T) {
	t.Run("loads all yaml files one level", func(t *testing.T) {
		testDir := t.TempDir()
		writeYAMLFileForApply(t, testDir, "app1.yaml", "name: app1\ntype: Make.App\nmeta:\n  version: 1.0.0\nproperties:\n  code: app1")
		writeYAMLFileForApply(t, testDir, "app2.yml", "name: app2\ntype: Make.App\nmeta:\n  version: 1.0.0\nproperties:\n  code: app2")
		// 创建嵌套目录 - 应被忽略
		os.Mkdir(filepath.Join(testDir, "nested"), 0755)
		writeYAMLFileForApply(t, filepath.Join(testDir, "nested"), "ignored.yaml", "name: ignored\ntype: Make.App")

		manifests, err := loadManifestsFromDir(testDir)
		if err != nil {
			t.Fatalf("loadManifestsFromDir: %v", err)
		}
		if len(manifests) != 2 {
			t.Fatalf("expected 2 manifests, got %d", len(manifests))
		}
	})

	t.Run("fails when directory has no recognized yaml files", func(t *testing.T) {
		testDir := t.TempDir()
		writeYAMLFileForApply(t, testDir, "app.json", `{"name":"app1"}`)
		writeYAMLFileForApply(t, testDir, "README.txt", "ignored")

		_, err := loadManifestsFromDir(testDir)
		if err == nil {
			t.Fatal("expected error for directory without yaml files")
		}

		want := "error reading [" + testDir + "]: recognized file extensions are [.yaml .yml]"
		if err.Error() != want {
			t.Fatalf("expected %q, got %q", want, err.Error())
		}
	})

	t.Run("skips hidden yaml files", func(t *testing.T) {
		testDir := t.TempDir()
		writeYAMLFileForApply(t, testDir, ".goreleaser.yml", "name: hidden\ntype: Make.App")
		writeYAMLFileForApply(t, testDir, "app.yaml", "name: visible\ntype: Make.App")

		manifests, err := loadManifestsFromDir(testDir)
		if err != nil {
			t.Fatalf("loadManifestsFromDir: %v", err)
		}
		if len(manifests) != 1 {
			t.Fatalf("expected 1 manifest, got %d", len(manifests))
		}
		if manifests[0].Name != "visible" {
			t.Fatalf("expected visible manifest, got %s", manifests[0].Name)
		}
	})

	t.Run("fails when only hidden yaml files exist", func(t *testing.T) {
		testDir := t.TempDir()
		writeYAMLFileForApply(t, testDir, ".goreleaser.yml", "name: hidden\ntype: Make.App")

		_, err := loadManifestsFromDir(testDir)
		if err == nil {
			t.Fatal("expected error for directory without visible yaml files")
		}

		want := "error reading [" + testDir + "]: recognized file extensions are [.yaml .yml]"
		if err.Error() != want {
			t.Fatalf("expected %q, got %q", want, err.Error())
		}
	})
}

// ---------------------------------- 辅助函数 ----------------------------------

// newMockMetaForApply 启动一个返回固定 code/message 的测试 Meta Server
func newMockMetaForApply(t *testing.T, code int, message string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"code":    code,
			"message": message,
			"data":    map[string]any{},
		})
	}))
}

// saveDefaultTokenForApply 在当前 HOME 下写入 default profile 的测试 JWT
func saveDefaultTokenForApply(t *testing.T) {
	t.Helper()
	fakeToken := "eyJ0eXAiOiJKV1QifQ.eyJzdWIiOiJ0ZXN0In0.c2lnbmF0dXJl"
	if err := config.Save(config.Credentials{
		"default": config.Profile{AccessToken: fakeToken},
	}); err != nil {
		t.Fatal(err)
	}
}

// writeYAMLFileForApply 在指定目录写入 YAML 文件，返回路径
func writeYAMLFileForApply(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}
