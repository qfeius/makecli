/**
 * [INPUT]: 依赖 cmd 包内的 runEntityCreate / loadEntityProperties / validateConstraintFieldRefs（包内白盒），internal/api、encoding/json、net/http、net/http/httptest、os、path/filepath、strings
 * [OUTPUT]: 覆盖 entity create 子命令核心逻辑的单元测试（含 --dry-run：X-Dry-Run 头到达线缆 + would-be 输出；--json properties：uniqueConstraints 到达线缆、未声明字段拒绝、validateConstraintFieldRefs 校验）
 * [POS]: cmd 模块 entity_create.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
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

	"github.com/qfeius/makecli/internal/api"
)

func TestRunEntityCreate(t *testing.T) {
	t.Run("creates entity with no fields", func(t *testing.T) {
		srv := newMockMeta(t, 200, "create entity success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		if err := runEntityCreate("project", "项目", "TODO", "", false); err != nil {
			t.Fatalf("runEntityCreate: %v", err)
		}
	})

	t.Run("creates entity with fields from file", func(t *testing.T) {
		srv := newMockMeta(t, 200, "create entity success")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		fieldsFile := writeFieldsFile(t, []map[string]any{
			{"key": "project_name", "name": "项目名称", "type": "Make.Field.Text", "meta": map[string]any{"version": "1.0.0"}, "properties": nil},
		})

		if err := runEntityCreate("project", "项目", "TODO", fieldsFile, false); err != nil {
			t.Fatalf("runEntityCreate with fields: %v", err)
		}
	})

	t.Run("rejects field key starting with underscore", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = "http://unused"

		fieldsFile := writeFieldsFile(t, []map[string]any{
			{"key": "_internal_field", "name": "内部字段", "type": "Make.Field.Text", "meta": map[string]any{"version": "1.0.0"}, "properties": nil},
		})

		if err := runEntityCreate("project", "项目", "TODO", fieldsFile, false); err == nil {
			t.Fatal("expected error for field key starting with _")
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		MetaServerURL = "http://unused"

		if err := runEntityCreate("project", "项目", "TODO", "", false); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails on API error response", func(t *testing.T) {
		srv := newMockMeta(t, 400, "invalid entity")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		if err := runEntityCreate("project", "项目", "TODO", "", false); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = "http://unused"
		setProfile(t, "nonexistent")

		if err := runEntityCreate("project", "项目", "TODO", "", false); err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})

	t.Run("dry-run sends X-Dry-Run and prints would-be line", func(t *testing.T) {
		var gotDryRun string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotDryRun = r.Header.Get("X-Dry-Run")
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "create entity success"})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		out := captureStdout(t, func() {
			if err := runEntityCreate("project", "项目", "TODO", "", true); err != nil {
				t.Fatalf("runEntityCreate dry-run: %v", err)
			}
		})
		if gotDryRun != "true" {
			t.Errorf("X-Dry-Run header = %q, want %q", gotDryRun, "true")
		}
		if !strings.Contains(out, "Dry run") || !strings.Contains(out, "would be created") {
			t.Errorf("dry-run output = %q, want a would-be 'Dry run' line", out)
		}
	})

	t.Run("fails with invalid fields file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = "http://unused"

		bad := filepath.Join(t.TempDir(), "bad.json")
		_ = os.WriteFile(bad, []byte("not json"), 0644)

		if err := runEntityCreate("project", "项目", "TODO", bad, false); err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}

func TestRunEntityCreateUniqueConstraints(t *testing.T) {
	t.Run("sends uniqueConstraints from properties file to the wire", func(t *testing.T) {
		var body map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&body)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "ok"})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		propsFile := writeEntityPropsFile(t, map[string]any{
			"fields": []map[string]any{
				{"key": "email", "name": "邮箱", "type": "Make.Field.Text", "meta": map[string]any{"version": "1.0.0"}, "properties": nil},
				{"key": "project_id", "name": "项目", "type": "Make.Field.Text", "meta": map[string]any{"version": "1.0.0"}, "properties": nil},
				{"key": "member_id", "name": "成员", "type": "Make.Field.Text", "meta": map[string]any{"version": "1.0.0"}, "properties": nil},
			},
			"uniqueConstraints": []map[string]any{
				{"name": "uniq_email", "fields": []string{"email"}},
				{"name": "uniq_pm", "fields": []string{"project_id", "member_id"}},
			},
		})

		if err := runEntityCreate("pm", "项目成员", "TODO", propsFile, false); err != nil {
			t.Fatalf("runEntityCreate: %v", err)
		}

		props, _ := body["properties"].(map[string]any)
		cons, ok := props["uniqueConstraints"].([]any)
		if !ok || len(cons) != 2 {
			t.Fatalf("uniqueConstraints on wire = %v, want 2 entries", props["uniqueConstraints"])
		}
		pm, _ := cons[1].(map[string]any)
		if pm["name"] != "uniq_pm" {
			t.Errorf("constraint[1].name = %v, want uniq_pm", pm["name"])
		}
		if fields, _ := pm["fields"].([]any); len(fields) != 2 || fields[0] != "project_id" || fields[1] != "member_id" {
			t.Errorf("constraint[1].fields = %v, want [project_id member_id]", pm["fields"])
		}
	})

	t.Run("rejects constraint referencing undeclared field before touching network", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = "http://unused"

		propsFile := writeEntityPropsFile(t, map[string]any{
			"fields": []map[string]any{
				{"key": "email", "name": "邮箱", "type": "Make.Field.Text", "meta": map[string]any{"version": "1.0.0"}, "properties": nil},
			},
			"uniqueConstraints": []map[string]any{
				{"name": "uniq_x", "fields": []string{"ghost"}},
			},
		})

		err := runEntityCreate("pm", "项目成员", "TODO", propsFile, false)
		if err == nil {
			t.Fatal("expected error for constraint referencing undeclared field")
		}
		if !strings.Contains(err.Error(), "ghost") {
			t.Errorf("error = %v, want it to name the missing field", err)
		}
	})
}

func TestValidateConstraintFieldRefs(t *testing.T) {
	props := func(fieldKeys []string, cons ...api.UniqueConstraint) api.EntityProperties {
		fields := make([]api.Field, len(fieldKeys))
		for i, k := range fieldKeys {
			fields[i] = api.Field{Key: k}
		}
		return api.EntityProperties{Fields: fields, UniqueConstraints: cons}
	}

	t.Run("no constraints → ok", func(t *testing.T) {
		if err := validateConstraintFieldRefs(props([]string{"a"})); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("all fields declared → ok", func(t *testing.T) {
		err := validateConstraintFieldRefs(props(
			[]string{"project_id", "member_id"},
			api.UniqueConstraint{Name: "uniq_pm", Fields: []string{"project_id", "member_id"}},
		))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("undeclared field → error naming it", func(t *testing.T) {
		err := validateConstraintFieldRefs(props(
			[]string{"email"},
			api.UniqueConstraint{Name: "uniq_x", Fields: []string{"ghost"}},
		))
		if err == nil || !strings.Contains(err.Error(), "ghost") {
			t.Fatalf("error = %v, want it to name the undeclared field", err)
		}
	})

	t.Run("empty fields → error", func(t *testing.T) {
		err := validateConstraintFieldRefs(props(
			[]string{"email"},
			api.UniqueConstraint{Name: "uniq_x", Fields: nil},
		))
		if err == nil {
			t.Fatal("expected error for constraint with no fields")
		}
	})
}

// writeFieldsFile 将 fields 包成 properties 对象 {fields:[...]} 写入临时 JSON 文件，返回路径
func writeFieldsFile(t *testing.T, fields []map[string]any) string {
	return writeEntityPropsFile(t, map[string]any{"fields": fields})
}

// writeEntityPropsFile 将 properties（fields + uniqueConstraints）写入临时 JSON 文件，返回路径
func writeEntityPropsFile(t *testing.T, props map[string]any) string {
	t.Helper()
	data, _ := json.Marshal(props)
	path := filepath.Join(t.TempDir(), "props.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}
