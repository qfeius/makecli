/**
 * [INPUT]: 依赖 cmd 包内函数（包内白盒）、internal/config、internal/api、encoding/json、net/http、net/http/httptest、os、path/filepath、testing
 * [OUTPUT]: 覆盖 diff 子命令核心逻辑的单元测试
 * [POS]: cmd 模块顶层 diff 命令的配套测试，用 httptest 隔离网络、临时文件测试差异对比
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

	"github.com/MakeHQ/makecli/internal/api"
	"github.com/MakeHQ/makecli/internal/config"
)

// ---------------------------------- diff 测试 ----------------------------------

func TestRunDiff(t *testing.T) {
	t.Run("fails with missing credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		dir := writeDiffYAML(t, entityYAML("Task", "myapp", "title", "Make.Field.Text"))

		err := runDiff(dir, "default", "http://localhost", outputTable)
		if err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDiffToken(t)
		dir := writeDiffYAML(t, entityYAML("Task", "myapp", "title", "Make.Field.Text"))

		err := runDiff(dir, "unknown", "http://localhost", outputTable)
		if err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})

	t.Run("fails when remote app not found", func(t *testing.T) {
		srv := newDiffServer(t, nil)
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDiffToken(t)
		dir := writeDiffYAML(t, entityYAML("Task", "myapp", "title", "Make.Field.Text"))

		err := runDiff(dir, "default", srv.URL, outputTable)
		if err == nil {
			t.Fatal("expected error when remote app not found")
		}
	})

	t.Run("fails with invalid output format", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		dir := writeDiffYAML(t, entityYAML("Task", "myapp", "title", "Make.Field.Text"))

		err := runDiff(dir, "default", "http://localhost", "xml")
		if err == nil {
			t.Fatal("expected error for invalid output format")
		}
	})
}

// ---------------------------------- computeDiff 测试 ----------------------------------

func TestComputeDiff(t *testing.T) {
	t.Run("no differences", func(t *testing.T) {
		local := []ResourceManifest{
			makeLocalEntity("Task", "title", "Make.Field.Text"),
		}
		remote := []api.Entity{
			makeRemoteEntity("Task", api.Field{Name: "title", Type: "Make.Field.Text"}),
		}

		result := computeDiff("myapp", local, remote)
		if result.Summary.Unchanged != 1 {
			t.Errorf("expected 1 unchanged, got %d", result.Summary.Unchanged)
		}
		if result.Summary.Added != 0 || result.Summary.Removed != 0 || result.Summary.Changed != 0 {
			t.Errorf("expected no diffs, got added=%d removed=%d changed=%d",
				result.Summary.Added, result.Summary.Removed, result.Summary.Changed)
		}
	})

	t.Run("entity only in local", func(t *testing.T) {
		local := []ResourceManifest{
			makeLocalEntity("NewEntity", "title", "Make.Field.Text"),
		}
		var remote []api.Entity

		result := computeDiff("myapp", local, remote)
		if result.Summary.Added != 1 {
			t.Errorf("expected 1 added, got %d", result.Summary.Added)
		}
		if result.Entities[0].Status != diffAdded {
			t.Errorf("expected status added, got %s", result.Entities[0].Status)
		}
	})

	t.Run("entity only on server", func(t *testing.T) {
		var local []ResourceManifest
		remote := []api.Entity{
			makeRemoteEntity("OldEntity", api.Field{Name: "title", Type: "Make.Field.Text"}),
		}

		result := computeDiff("myapp", local, remote)
		if result.Summary.Removed != 1 {
			t.Errorf("expected 1 removed, got %d", result.Summary.Removed)
		}
		if result.Entities[0].Status != diffRemoved {
			t.Errorf("expected status removed, got %s", result.Entities[0].Status)
		}
	})

	t.Run("field type changed", func(t *testing.T) {
		local := []ResourceManifest{
			makeLocalEntity("Task", "description", "Make.Field.TextArea"),
		}
		remote := []api.Entity{
			makeRemoteEntity("Task", api.Field{Name: "description", Type: "Make.Field.Text"}),
		}

		result := computeDiff("myapp", local, remote)
		if result.Summary.Changed != 1 {
			t.Errorf("expected 1 changed, got %d", result.Summary.Changed)
		}
		entity := result.Entities[0]
		if len(entity.Fields) != 1 {
			t.Fatalf("expected 1 field diff, got %d", len(entity.Fields))
		}
		if entity.Fields[0].Status != diffChanged {
			t.Errorf("expected field status changed, got %s", entity.Fields[0].Status)
		}
		if entity.Fields[0].Detail != "type: Make.Field.Text → Make.Field.TextArea" {
			t.Errorf("unexpected detail: %s", entity.Fields[0].Detail)
		}
	})

	t.Run("field added in local", func(t *testing.T) {
		local := []ResourceManifest{
			makeLocalEntityMultiFields("Task", []fieldDef{
				{"title", "Make.Field.Text"},
				{"newField", "Make.Field.Number"},
			}),
		}
		remote := []api.Entity{
			makeRemoteEntity("Task", api.Field{Name: "title", Type: "Make.Field.Text"}),
		}

		result := computeDiff("myapp", local, remote)
		if result.Summary.Changed != 1 {
			t.Errorf("expected 1 changed, got %d", result.Summary.Changed)
		}
		var addedField *FieldDiff
		for _, f := range result.Entities[0].Fields {
			if f.Name == "newField" {
				addedField = &f
				break
			}
		}
		if addedField == nil {
			t.Fatal("expected to find newField in diff")
		}
		if addedField.Status != diffAdded {
			t.Errorf("expected added, got %s", addedField.Status)
		}
	})

	t.Run("field removed from local", func(t *testing.T) {
		local := []ResourceManifest{
			makeLocalEntity("Task", "title", "Make.Field.Text"),
		}
		remote := []api.Entity{
			makeRemoteEntity("Task",
				api.Field{Name: "title", Type: "Make.Field.Text"},
				api.Field{Name: "oldField", Type: "Make.Field.Number"},
			),
		}

		result := computeDiff("myapp", local, remote)
		if result.Summary.Changed != 1 {
			t.Errorf("expected 1 changed, got %d", result.Summary.Changed)
		}
		var removedField *FieldDiff
		for _, f := range result.Entities[0].Fields {
			if f.Name == "oldField" {
				removedField = &f
				break
			}
		}
		if removedField == nil {
			t.Fatal("expected to find oldField in diff")
		}
		if removedField.Status != diffRemoved {
			t.Errorf("expected removed, got %s", removedField.Status)
		}
	})

	t.Run("mixed scenario", func(t *testing.T) {
		local := []ResourceManifest{
			makeLocalEntity("Unchanged", "title", "Make.Field.Text"),
			makeLocalEntity("Changed", "desc", "Make.Field.TextArea"),
			makeLocalEntity("OnlyLocal", "name", "Make.Field.Text"),
		}
		remote := []api.Entity{
			makeRemoteEntity("Unchanged", api.Field{Name: "title", Type: "Make.Field.Text"}),
			makeRemoteEntity("Changed", api.Field{Name: "desc", Type: "Make.Field.Text"}),
			makeRemoteEntity("OnlyServer", api.Field{Name: "name", Type: "Make.Field.Text"}),
		}

		result := computeDiff("myapp", local, remote)
		if result.Summary.Unchanged != 1 {
			t.Errorf("expected 1 unchanged, got %d", result.Summary.Unchanged)
		}
		if result.Summary.Changed != 1 {
			t.Errorf("expected 1 changed, got %d", result.Summary.Changed)
		}
		if result.Summary.Added != 1 {
			t.Errorf("expected 1 added, got %d", result.Summary.Added)
		}
		if result.Summary.Removed != 1 {
			t.Errorf("expected 1 removed, got %d", result.Summary.Removed)
		}
	})

	t.Run("sort order: changed > added > removed > unchanged", func(t *testing.T) {
		local := []ResourceManifest{
			makeLocalEntity("Unchanged", "title", "Make.Field.Text"),
			makeLocalEntity("Added", "name", "Make.Field.Text"),
			makeLocalEntity("Changed", "desc", "Make.Field.TextArea"),
		}
		remote := []api.Entity{
			makeRemoteEntity("Unchanged", api.Field{Name: "title", Type: "Make.Field.Text"}),
			makeRemoteEntity("Removed", api.Field{Name: "name", Type: "Make.Field.Text"}),
			makeRemoteEntity("Changed", api.Field{Name: "desc", Type: "Make.Field.Text"}),
		}

		result := computeDiff("myapp", local, remote)
		expected := []string{"Changed", "Added", "Removed", "Unchanged"}
		for i, e := range result.Entities {
			if e.Name != expected[i] {
				t.Errorf("position %d: expected %s, got %s", i, expected[i], e.Name)
			}
		}
	})
}

// ---------------------------------- fetchAllEntities 测试 ----------------------------------

func TestFetchAllEntities(t *testing.T) {
	t.Run("single page", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200,
				"msg":  "ok",
				"data": []map[string]any{
					{"name": "E1", "type": "Make.Entity", "app": "a", "meta": map[string]any{}, "properties": map[string]any{"fields": []any{}}},
					{"name": "E2", "type": "Make.Entity", "app": "a", "meta": map[string]any{}, "properties": map[string]any{"fields": []any{}}},
				},
				"pagination": map[string]any{"total": 2},
			})
		}))
		defer srv.Close()

		client := api.New(srv.URL, "tok")
		entities, err := fetchAllEntities(client, "a")
		if err != nil {
			t.Fatalf("fetchAllEntities: %v", err)
		}
		if len(entities) != 2 {
			t.Fatalf("expected 2, got %d", len(entities))
		}
	})

	t.Run("multi page", func(t *testing.T) {
		callCount := 0
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			callCount++
			w.Header().Set("Content-Type", "application/json")

			var data []map[string]any
			if callCount == 1 {
				data = []map[string]any{
					{"name": "E1", "type": "Make.Entity", "app": "a", "meta": map[string]any{}, "properties": map[string]any{"fields": []any{}}},
				}
			} else {
				data = []map[string]any{
					{"name": "E2", "type": "Make.Entity", "app": "a", "meta": map[string]any{}, "properties": map[string]any{"fields": []any{}}},
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":       200,
				"msg":        "ok",
				"data":       data,
				"pagination": map[string]any{"total": 2},
			})
		}))
		defer srv.Close()

		client := api.New(srv.URL, "tok")
		entities, err := fetchAllEntities(client, "a")
		if err != nil {
			t.Fatalf("fetchAllEntities: %v", err)
		}
		if len(entities) != 2 {
			t.Fatalf("expected 2, got %d", len(entities))
		}
		if callCount != 2 {
			t.Fatalf("expected 2 API calls, got %d", callCount)
		}
	})
}

// ---------------------------------- jsonDeepEqual 测试 ----------------------------------

func TestJsonDeepEqual(t *testing.T) {
	t.Run("nil vs nil", func(t *testing.T) {
		if !jsonDeepEqual(nil, nil) {
			t.Error("nil should equal nil")
		}
	})

	t.Run("int vs float64 normalization", func(t *testing.T) {
		// YAML 解析 int, JSON 解析 float64
		if !jsonDeepEqual(42, 42.0) {
			t.Error("42 should equal 42.0 after normalization")
		}
	})

	t.Run("map comparison", func(t *testing.T) {
		a := map[string]any{"key": 1}
		b := map[string]any{"key": 1.0}
		if !jsonDeepEqual(a, b) {
			t.Error("maps should be equal after normalization")
		}
	})

	t.Run("different values", func(t *testing.T) {
		if jsonDeepEqual("a", "b") {
			t.Error("different strings should not be equal")
		}
	})
}

// ---------------------------------- 辅助函数 ----------------------------------

// fieldDef 测试用字段定义
type fieldDef struct {
	Name string
	Type string
}

// makeLocalEntity 构造包含单个字段的本地 Entity Manifest
func makeLocalEntity(name, fieldName, fieldType string) ResourceManifest {
	return makeLocalEntityMultiFields(name, []fieldDef{{fieldName, fieldType}})
}

// makeLocalEntityMultiFields 构造包含多个字段的本地 Entity Manifest
func makeLocalEntityMultiFields(name string, fields []fieldDef) ResourceManifest {
	fs := make([]any, len(fields))
	for i, f := range fields {
		fs[i] = map[string]any{
			"name":       f.Name,
			"type":       f.Type,
			"meta":       map[string]any{"version": "1.0.0"},
			"properties": nil,
		}
	}
	return ResourceManifest{
		Name: name,
		Type: "Make.Entity",
		App:  "myapp",
		Meta: map[string]any{"version": "1.0.0"},
		Properties: map[string]any{
			"fields": fs,
		},
	}
}

// makeRemoteEntity 构造远端 Entity 对象
func makeRemoteEntity(name string, fields ...api.Field) api.Entity {
	return api.Entity{
		Name: name,
		Type: "Make.Entity",
		App:  "myapp",
		Meta: map[string]any{"version": "1.0.0"},
		Properties: api.EntityProperties{
			Fields: fields,
		},
	}
}

// entityYAML 生成单 Entity 的 YAML 字符串
func entityYAML(name, app, fieldName, fieldType string) string {
	return `name: ` + name + `
type: Make.Entity
app: ` + app + `
meta:
  version: 1.0.0
properties:
  fields:
    - name: ` + fieldName + `
      type: ` + fieldType + `
      meta:
        version: 1.0.0
      properties: null
`
}

// writeDiffYAML 写入 YAML 到临时目录，返回目录路径
func writeDiffYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "entity.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// saveDiffToken 写入测试用凭证
func saveDiffToken(t *testing.T) {
	t.Helper()
	fakeToken := "eyJ0eXAiOiJKV1QifQ.eyJzdWIiOiJ0ZXN0In0.c2lnbmF0dXJl"
	if err := config.Save(config.Credentials{
		"default": config.Profile{AccessToken: fakeToken},
	}); err != nil {
		t.Fatal(err)
	}
}

// newDiffServer 创建 mock Meta Server，根据 X-Make-Target 路由请求
// remoteEntities 为 nil 时 GetApp 返回 404（app 不存在）
func newDiffServer(t *testing.T, remoteEntities []api.Entity) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := r.Header.Get("X-Make-Target")
		w.Header().Set("Content-Type", "application/json")

		switch target {
		case "MakeService.GetResource":
			if remoteEntities == nil {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code": 404,
					"msg":  "not found",
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200,
				"msg":  "ok",
				"data": map[string]any{
					"name":       "myapp",
					"type":       "Make.App",
					"meta":       map[string]any{"version": "1.0.0"},
					"properties": map[string]any{},
				},
			})

		case "MakeService.ListResources":
			entities := remoteEntities
			if entities == nil {
				entities = []api.Entity{}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":       200,
				"msg":        "ok",
				"data":       entities,
				"pagination": map[string]any{"total": len(entities)},
			})

		default:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 400,
				"msg":  "unknown target",
			})
		}
	}))
}
