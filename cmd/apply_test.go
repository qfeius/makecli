/**
 * [INPUT]: 依赖 cmd 包内函数（包内白盒）、internal/config、encoding/json、net/http、net/http/httptest、os、path/filepath、strconv、strings、testing
 * [OUTPUT]: 覆盖 apply 子命令核心逻辑的单元测试（App/Entity/Relation，含 ErrNotFound 幂等性：瞬时错误不创建/not-found 创建/已存在更新；uniqueConstraints 到达线缆 + extractUniqueConstraints 解析/错误）
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
	"strconv"
	"strings"
	"testing"

	"github.com/qfeius/makecli/internal/config"
)

// ---------------------------------- apply 测试 ----------------------------------

func TestRunAppApply(t *testing.T) {
	t.Run("applies single app from file", func(t *testing.T) {
		srv := newMockMetaForApply(t, 200, "create app success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultTokenForApply(t)
		MetaServerURL = srv.URL
		testDir := t.TempDir()

		yamlFile := writeYAMLFileForApply(t, testDir, "app.yaml", `key: myapp
name: 我的应用
type: Make.App
meta:
  version: 1.0.0
properties:
  description: demo
`)

		if err := runAppApply(yamlFile); err != nil {
			t.Fatalf("runAppApply: %v", err)
		}
	})

	t.Run("applies multi-document YAML", func(t *testing.T) {
		callCount := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callCount++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"code":200,"msg":"success","data":{}}`))
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultTokenForApply(t)
		MetaServerURL = srv.URL
		testDir := t.TempDir()

		yamlFile := writeYAMLFileForApply(t, testDir, "multi.yaml", `key: app1
name: 应用一
type: Make.App
meta:
  version: 1.0.0
properties:
  description: app1
---
key: app2
name: 应用二
type: Make.App
meta:
  version: 1.0.0
properties:
  description: app2
`)

		if err := runAppApply(yamlFile); err != nil {
			t.Fatalf("runAppApply multi-doc: %v", err)
		}
		// 每个 App: 1x GetApp + 1x CreateApp = 2 calls，2 个 App = 4 calls
		if callCount != 4 {
			t.Fatalf("expected 4 API calls, got %d", callCount)
		}
	})

	t.Run("applies app then entity from directory", func(t *testing.T) {
		callCount := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callCount++
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"code":200,"msg":"success","data":{}}`))
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultTokenForApply(t)
		MetaServerURL = srv.URL
		testDir := t.TempDir()

		writeYAMLFileForApply(t, testDir, "app.yaml", `key: myapp
name: 我的应用
type: Make.App
meta:
  version: 1.0.0
properties:
  description: myapp
`)
		writeYAMLFileForApply(t, testDir, "entity.yaml", `key: task
name: 任务
type: Make.Entity
appKey: myapp
meta:
  version: 1.0.0
properties:
  fields:
    - key: title
      name: 标题
      type: Make.Field.Text
      meta:
        version: 1.0.0
      properties: {}
`)

		if err := runAppApply(testDir); err != nil {
			t.Fatalf("runAppApply dir: %v", err)
		}
		// 1x GetApp + 1x CreateApp + 1x GetEntity + 1x CreateEntity = 4 calls
		if callCount != 4 {
			t.Fatalf("expected 4 API calls, got %d", callCount)
		}
	})

	t.Run("applies relation from file", func(t *testing.T) {
		callCount := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callCount++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":200,"msg":"success","data":{}}`))
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultTokenForApply(t)
		MetaServerURL = srv.URL
		testDir := t.TempDir()

		yamlFile := writeYAMLFileForApply(t, testDir, "relation.yaml", `key: project_has_tasks
name: 项目任务关联
type: Make.Relation
appKey: myapp
meta:
  version: 1.0.0
properties:
  from:
    entityKey: project
    cardinality: one
  to:
    entityKey: task
    cardinality: many
`)

		if err := runAppApply(yamlFile); err != nil {
			t.Fatalf("runAppApply relation: %v", err)
		}
		// 1x GetRelation + 1x CreateRelation = 2 calls
		if callCount != 2 {
			t.Fatalf("expected 2 API calls, got %d", callCount)
		}
	})

	t.Run("updates existing relation", func(t *testing.T) {
		callCount := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			callCount++
			w.Header().Set("Content-Type", "application/json")
			target := r.Header.Get("X-Make-Target")
			if target == "MakeService.GetResource" {
				_, _ = w.Write([]byte(`{"code":200,"msg":"ok","data":{"key":"project_has_tasks","name":"项目任务关联","type":"Make.Relation","appKey":"myapp"}}`))
			} else {
				_, _ = w.Write([]byte(`{"code":200,"msg":"success","data":{}}`))
			}
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultTokenForApply(t)
		MetaServerURL = srv.URL
		testDir := t.TempDir()

		yamlFile := writeYAMLFileForApply(t, testDir, "relation.yaml", `key: project_has_tasks
name: 项目任务关联
type: Make.Relation
appKey: myapp
meta:
  version: 1.0.0
properties:
  from:
    entityKey: project
    cardinality: one
  to:
    entityKey: task
    cardinality: many
`)

		if err := runAppApply(yamlFile); err != nil {
			t.Fatalf("runAppApply update relation: %v", err)
		}
		// 1x GetRelation + 1x UpdateRelation = 2 calls
		if callCount != 2 {
			t.Fatalf("expected 2 API calls, got %d", callCount)
		}
	})

	t.Run("fails with relation missing appKey", func(t *testing.T) {
		srv := newMockMetaForApply(t, 200, "success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultTokenForApply(t)
		MetaServerURL = srv.URL
		testDir := t.TempDir()

		yamlFile := writeYAMLFileForApply(t, testDir, "relation.yaml", `key: project_has_tasks
name: 项目任务关联
type: Make.Relation
meta:
  version: 1.0.0
properties:
  from:
    entityKey: project
    cardinality: one
  to:
    entityKey: task
    cardinality: many
`)

		if err := runAppApply(yamlFile); err == nil {
			t.Fatal("expected error for missing appKey field")
		}
	})

	t.Run("fails with relation missing from", func(t *testing.T) {
		srv := newMockMetaForApply(t, 200, "success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultTokenForApply(t)
		MetaServerURL = srv.URL
		testDir := t.TempDir()

		yamlFile := writeYAMLFileForApply(t, testDir, "relation.yaml", `key: project_has_tasks
name: 项目任务关联
type: Make.Relation
appKey: myapp
meta:
  version: 1.0.0
properties:
  to:
    entityKey: task
    cardinality: many
`)

		if err := runAppApply(yamlFile); err == nil {
			t.Fatal("expected error for missing from field")
		}
	})

	t.Run("applies app + entity + relation from directory", func(t *testing.T) {
		callCount := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callCount++
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code":200,"msg":"success","data":{}}`))
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultTokenForApply(t)
		MetaServerURL = srv.URL
		testDir := t.TempDir()

		writeYAMLFileForApply(t, testDir, "app.yaml", `key: myapp
name: 我的应用
type: Make.App
meta:
  version: 1.0.0
properties:
  description: myapp
`)
		writeYAMLFileForApply(t, testDir, "entity.yaml", `key: task
name: 任务
type: Make.Entity
appKey: myapp
meta:
  version: 1.0.0
properties:
  fields:
    - key: title
      name: 标题
      type: Make.Field.Text
      meta:
        version: 1.0.0
      properties: {}
`)
		writeYAMLFileForApply(t, testDir, "relation.yaml", `key: project_has_tasks
name: 项目任务关联
type: Make.Relation
appKey: myapp
meta:
  version: 1.0.0
properties:
  from:
    entityKey: project
    cardinality: one
  to:
    entityKey: task
    cardinality: many
`)

		if err := runAppApply(testDir); err != nil {
			t.Fatalf("runAppApply dir with relation: %v", err)
		}
		// 2(App) + 2(Entity) + 2(Relation) = 6 calls
		if callCount != 6 {
			t.Fatalf("expected 6 API calls, got %d", callCount)
		}
	})

	t.Run("fails with missing credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		MetaServerURL = "http://unused"
		testDir := t.TempDir()
		// 不写入凭证，测试缺失凭证的情况

		yamlFile := writeYAMLFileForApply(t, testDir, "app.yaml", `key: test
name: 测试应用
type: Make.App
meta:
  version: 1.0.0
properties:
  description: test
`)

		if err := runAppApply(yamlFile); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		testDir := t.TempDir()
		saveDefaultTokenForApply(t)
		MetaServerURL = "http://unused"
		setProfile(t, "unknown")
		yamlFile := writeYAMLFileForApply(t, testDir, "app.yaml", `key: test
name: 测试应用
type: Make.App
meta:
  version: 1.0.0
properties:
  description: test
`)

		if err := runAppApply(yamlFile); err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})

	t.Run("fails on API error", func(t *testing.T) {
		srv := newMockMetaForApply(t, 400, "invalid app")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultTokenForApply(t)
		MetaServerURL = srv.URL
		testDir := t.TempDir()

		yamlFile := writeYAMLFileForApply(t, testDir, "app.yaml", `key: test
name: 测试应用
type: Make.App
meta:
  version: 1.0.0
properties:
  description: test
`)

		if err := runAppApply(yamlFile); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails with entity missing app", func(t *testing.T) {
		srv := newMockMetaForApply(t, 200, "create entity success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultTokenForApply(t)
		MetaServerURL = srv.URL
		testDir := t.TempDir()

		yamlFile := writeYAMLFileForApply(t, testDir, "entity.yaml", `key: task
name: 任务
type: Make.Entity
meta:
  version: 1.0.0
properties:
  fields: []
`)

		if err := runAppApply(yamlFile); err == nil {
			t.Fatal("expected error for missing appKey field")
		}
	})

	t.Run("fails on unknown resource type", func(t *testing.T) {
		srv := newMockMetaForApply(t, 200, "success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultTokenForApply(t)
		MetaServerURL = srv.URL
		testDir := t.TempDir()

		yamlFile := writeYAMLFileForApply(t, testDir, "app.yaml", `key: todo
name: 待办
type: aaa.App
meta:
  version: 1.0.0
properties:
  description: todo
`)

		err := runAppApply(yamlFile)
		if err == nil {
			t.Fatal("expected error for unknown resource type")
		}
		if !strings.Contains(err.Error(), "未知资源类型") {
			t.Fatalf("expected unknown type error, got %q", err.Error())
		}
	})

	t.Run("fails on empty YAML file", func(t *testing.T) {
		srv := newMockMetaForApply(t, 200, "success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultTokenForApply(t)
		MetaServerURL = srv.URL
		testDir := t.TempDir()

		yamlFile := writeYAMLFileForApply(t, testDir, "empty.yaml", "")

		err := runAppApply(yamlFile)
		if err == nil {
			t.Fatal("expected error for empty YAML file")
		}
		want := "no objects passed to apply"
		if err.Error() != want {
			t.Fatalf("expected %q, got %q", want, err.Error())
		}
	})

	t.Run("fails on invalid YAML", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultTokenForApply(t)
		MetaServerURL = "http://unused"
		testDir := t.TempDir()
		bad := filepath.Join(testDir, "bad.yaml")
		_ = os.WriteFile(bad, []byte("invalid: yaml: ["), 0644)

		if err := runAppApply(bad); err == nil {
			t.Fatal("expected error for invalid YAML")
		}
	})
}

// ---------------------------------- create-or-update 幂等性（ErrNotFound 语义） ----------------------------------

// TestRunAppApplyDoesNotCreateOnTransientGetError 锁定核心缺陷修复：
// 当 Get 因瞬时/传输/非 not-found 业务错误失败时，apply 必须上抛错误且绝不调用 Create，
// 杜绝把"已存在的 App"误建成重复键，或把 Entity/Relation 的 update 静默降级为 create。
func TestRunAppApplyDoesNotCreateOnTransientGetError(t *testing.T) {
	cases := []struct {
		name    string
		yaml    string
		getCode int // GetResource 返回的业务码（非 200/非 404）
	}{
		{
			name:    "app get 500 does not create",
			getCode: 500,
			yaml: `key: myapp
name: 我的应用
type: Make.App
meta:
  version: 1.0.0
properties:
  description: demo
`,
		},
		{
			name:    "entity get 500 does not create",
			getCode: 500,
			yaml: `key: task
name: 任务
type: Make.Entity
appKey: myapp
meta:
  version: 1.0.0
properties:
  fields:
    - key: title
      name: 标题
      type: Make.Field.Text
      meta:
        version: 1.0.0
      properties: {}
`,
		},
		{
			name:    "relation get 500 does not create",
			getCode: 500,
			yaml: `key: project_has_tasks
name: 项目任务关联
type: Make.Relation
appKey: myapp
meta:
  version: 1.0.0
properties:
  from:
    entityKey: project
    cardinality: one
  to:
    entityKey: task
    cardinality: many
`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			createHits := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				target := r.Header.Get("X-Make-Target")
				if target == "MakeService.GetResource" {
					// 瞬时故障：非 200 且非 404 的业务码
					_, _ = w.Write([]byte(`{"code":` + itoaForApply(tc.getCode) + `,"msg":"transient failure","data":{}}`))
					return
				}
				if target == "MakeService.CreateResource" {
					createHits++
				}
				_, _ = w.Write([]byte(`{"code":200,"msg":"ok","data":{}}`))
			}))
			defer srv.Close()
			t.Setenv("HOME", t.TempDir())
			saveDefaultTokenForApply(t)
			MetaServerURL = srv.URL
			testDir := t.TempDir()

			yamlFile := writeYAMLFileForApply(t, testDir, "res.yaml", tc.yaml)

			err := runAppApply(yamlFile)
			if err == nil {
				t.Fatal("expected error to propagate from transient Get failure")
			}
			if createHits != 0 {
				t.Fatalf("Create must NOT be called on transient Get error, got %d hits", createHits)
			}
		})
	}
}

// TestRunAppApplyCreatesOnNotFound 验证 Get 返回 not-found 业务码（404）时走 Create 分支。
func TestRunAppApplyCreatesOnNotFound(t *testing.T) {
	createHits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		target := r.Header.Get("X-Make-Target")
		if target == "MakeService.GetResource" {
			_, _ = w.Write([]byte(`{"code":404,"msg":"not found","data":{}}`))
			return
		}
		if target == "MakeService.CreateResource" {
			createHits++
		}
		_, _ = w.Write([]byte(`{"code":200,"msg":"ok","data":{}}`))
	}))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	saveDefaultTokenForApply(t)
	MetaServerURL = srv.URL
	testDir := t.TempDir()

	yamlFile := writeYAMLFileForApply(t, testDir, "app.yaml", `key: myapp
name: 我的应用
type: Make.App
meta:
  version: 1.0.0
properties:
  description: demo
`)

	if err := runAppApply(yamlFile); err != nil {
		t.Fatalf("runAppApply on not-found: %v", err)
	}
	if createHits != 1 {
		t.Fatalf("expected exactly 1 Create on not-found, got %d", createHits)
	}
}

// TestRunAppApplyUpdatesOnExisting 验证 Get 返回存在资源时 Entity 走 Update（而非 Create）。
func TestRunAppApplyUpdatesOnExisting(t *testing.T) {
	updateHits := 0
	createHits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Header.Get("X-Make-Target") {
		case "MakeService.GetResource":
			_, _ = w.Write([]byte(`{"code":200,"msg":"ok","data":{"key":"task","name":"任务","appKey":"myapp"}}`))
		case "MakeService.UpdateResource":
			updateHits++
			_, _ = w.Write([]byte(`{"code":200,"msg":"ok","data":{}}`))
		case "MakeService.CreateResource":
			createHits++
			_, _ = w.Write([]byte(`{"code":200,"msg":"ok","data":{}}`))
		default:
			_, _ = w.Write([]byte(`{"code":200,"msg":"ok","data":{}}`))
		}
	}))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	saveDefaultTokenForApply(t)
	MetaServerURL = srv.URL
	testDir := t.TempDir()

	yamlFile := writeYAMLFileForApply(t, testDir, "entity.yaml", `key: task
name: 任务
type: Make.Entity
appKey: myapp
meta:
  version: 1.0.0
properties:
  fields:
    - key: title
      name: 标题
      type: Make.Field.Text
      meta:
        version: 1.0.0
      properties: {}
`)

	if err := runAppApply(yamlFile); err != nil {
		t.Fatalf("runAppApply on existing: %v", err)
	}
	if updateHits != 1 || createHits != 0 {
		t.Fatalf("expected 1 Update + 0 Create on existing, got update=%d create=%d", updateHits, createHits)
	}
}

func TestRunAppApplyFailsWithoutRecognizedYAMLFiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	saveDefaultTokenForApply(t)
	MetaServerURL = "http://unused"
	testDir := t.TempDir()
	writeYAMLFileForApply(t, testDir, "app.json", `{"name":"app1"}`)

	err := runAppApply(testDir)
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
		data := `key: myapp
name: 我的应用
type: Make.App
meta:
  version: 1.0.0
properties:
  description: myapp
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
		if manifests[0].Key != "myapp" {
			t.Errorf("expected key myapp, got %s", manifests[0].Key)
		}
	})

	t.Run("loads multi-document", func(t *testing.T) {
		data := `key: app1
name: 应用一
type: Make.App
meta:
  version: 1.0.0
properties:
  description: app1
---
key: app2
name: 应用二
type: Make.App
meta:
  version: 1.0.0
properties:
  description: app2
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
key: app2
name: 应用二
type: Make.App
meta:
  version: 1.0.0
properties:
  description: app2
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
		if manifests[0].Key != "app2" {
			t.Errorf("expected app2, got %s", manifests[0].Key)
		}
	})
}

// ---------------------------------- loadManifestsFromDir 测试 ----------------------------------

func TestLoadManifestsFromDir(t *testing.T) {
	t.Run("loads all yaml files one level", func(t *testing.T) {
		testDir := t.TempDir()
		writeYAMLFileForApply(t, testDir, "app1.yaml", "key: app1\nname: 应用一\ntype: Make.App\nmeta:\n  version: 1.0.0\nproperties:\n  description: app1")
		writeYAMLFileForApply(t, testDir, "app2.yml", "key: app2\nname: 应用二\ntype: Make.App\nmeta:\n  version: 1.0.0\nproperties:\n  description: app2")
		// 创建嵌套目录 - 应被忽略
		_ = os.Mkdir(filepath.Join(testDir, "nested"), 0755)
		writeYAMLFileForApply(t, filepath.Join(testDir, "nested"), "ignored.yaml", "key: ignored\nname: 忽略\ntype: Make.App")

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
		writeYAMLFileForApply(t, testDir, ".goreleaser.yml", "key: hidden\nname: 隐藏\ntype: Make.App")
		writeYAMLFileForApply(t, testDir, "app.yaml", "key: visible\nname: 可见\ntype: Make.App")

		manifests, err := loadManifestsFromDir(testDir)
		if err != nil {
			t.Fatalf("loadManifestsFromDir: %v", err)
		}
		if len(manifests) != 1 {
			t.Fatalf("expected 1 manifest, got %d", len(manifests))
		}
		if manifests[0].Key != "visible" {
			t.Fatalf("expected visible manifest, got %s", manifests[0].Key)
		}
	})

	t.Run("fails when only hidden yaml files exist", func(t *testing.T) {
		testDir := t.TempDir()
		writeYAMLFileForApply(t, testDir, ".goreleaser.yml", "key: hidden\nname: 隐藏\ntype: Make.App")

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

// ---------------------------------- uniqueConstraints 测试 ----------------------------------

func TestApplyEntitySendsUniqueConstraints(t *testing.T) {
	var createBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("X-Make-Target") == "MakeService.GetResource" {
			_, _ = w.Write([]byte(`{"code":200,"msg":"ok","data":{}}`)) // 空 data → ErrNotFound → 触发 create
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&createBody)
		_, _ = w.Write([]byte(`{"code":200,"msg":"success","data":{}}`))
	}))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	saveDefaultTokenForApply(t)
	MetaServerURL = srv.URL
	testDir := t.TempDir()

	writeYAMLFileForApply(t, testDir, "entity.yaml", `key: pm
name: 项目成员
type: Make.Entity
appKey: myapp
meta:
  version: 1.0.0
properties:
  fields:
    - key: project_id
      name: 项目
      type: Make.Field.Text
      meta:
        version: 1.0.0
      properties: {}
    - key: member_id
      name: 成员
      type: Make.Field.Text
      meta:
        version: 1.0.0
      properties: {}
  uniqueConstraints:
    - name: uniq_pm
      fields:
        - project_id
        - member_id
`)

	if err := runAppApply(testDir); err != nil {
		t.Fatalf("runAppApply: %v", err)
	}

	props, _ := createBody["properties"].(map[string]any)
	cons, ok := props["uniqueConstraints"].([]any)
	if !ok || len(cons) != 1 {
		t.Fatalf("uniqueConstraints on wire = %v, want 1 entry", props["uniqueConstraints"])
	}
	c0, _ := cons[0].(map[string]any)
	if c0["name"] != "uniq_pm" {
		t.Errorf("constraint name = %v, want uniq_pm", c0["name"])
	}
	if fields, _ := c0["fields"].([]any); len(fields) != 2 || fields[0] != "project_id" {
		t.Errorf("constraint fields = %v, want [project_id member_id]", c0["fields"])
	}
}

func TestExtractUniqueConstraints(t *testing.T) {
	t.Run("absent → nil", func(t *testing.T) {
		got, err := extractUniqueConstraints(map[string]any{"fields": []any{}})
		if err != nil || got != nil {
			t.Fatalf("got (%v, %v), want (nil, nil)", got, err)
		}
	})

	t.Run("parses name and fields", func(t *testing.T) {
		got, err := extractUniqueConstraints(map[string]any{
			"uniqueConstraints": []any{
				map[string]any{"name": "uniq_email", "fields": []any{"email"}},
			},
		})
		if err != nil {
			t.Fatalf("extractUniqueConstraints: %v", err)
		}
		if len(got) != 1 || got[0].Name != "uniq_email" || len(got[0].Fields) != 1 || got[0].Fields[0] != "email" {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("non-array → error", func(t *testing.T) {
		if _, err := extractUniqueConstraints(map[string]any{"uniqueConstraints": "nope"}); err == nil {
			t.Fatal("expected error for non-array uniqueConstraints")
		}
	})

	t.Run("missing fields key → error", func(t *testing.T) {
		_, err := extractUniqueConstraints(map[string]any{
			"uniqueConstraints": []any{map[string]any{"name": "x"}},
		})
		if err == nil {
			t.Fatal("expected error for constraint missing fields")
		}
	})
}

// ---------------------------------- 辅助函数 ----------------------------------

// newMockMetaForApply 启动一个返回固定 code/message 的测试 Meta Server
func newMockMetaForApply(t *testing.T, code int, message string) *httptest.Server {
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

// itoaForApply 把业务码拼进 JSON body（避免为单测引入 strconv import）
func itoaForApply(n int) string {
	return strconv.Itoa(n)
}
