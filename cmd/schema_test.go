/**
 * [INPUT]: 依赖 cmd 包内的 runSchema（包内白盒），internal/config、encoding/json、net/http、net/http/httptest、strings
 * [OUTPUT]: 覆盖 schema 子命令核心逻辑的单元测试
 * [POS]: cmd 模块 schema.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunSchema(t *testing.T) {
	t.Run("returns schema successfully", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Make-Target") != "MakeService.GetResource" {
				t.Errorf("unexpected X-Make-Target: %s", r.Header.Get("X-Make-Target"))
			}
			if r.URL.Path != "/meta/v1/schema" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			var req map[string]string
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req["app"] != "报销管理" {
				t.Errorf("unexpected app: %s", req["app"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "get app schema success",
				"data": map[string]any{
					"app": map[string]any{
						"name": "报销管理", "type": "Make.App",
						"meta":       map[string]any{"version": "1.0.0"},
						"properties": map[string]any{"renderName": "expense_management"},
					},
					"entities": []map[string]any{
						{"name": "expense_report", "type": "Make.Entity", "app": "报销管理",
							"meta":       map[string]any{"version": "1.0.0"},
							"properties": map[string]any{"fields": []map[string]any{{"name": "申请人", "type": "Make.Field.Text"}}}},
					},
					"relations": []map[string]any{
						{"name": "report_has_invoices", "type": "Make.Relation", "app": "报销管理",
							"meta": map[string]any{"version": "1.0.0"},
							"properties": map[string]any{
								"from": map[string]any{"entity": "expense_report", "cardinality": "one"},
								"to":   map[string]any{"entity": "expense_invoice", "cardinality": "many"},
							}},
					},
				},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		output := captureStdout(t, func() {
			if err := runSchema("报销管理", "default"); err != nil {
				t.Fatalf("runSchema: %v", err)
			}
		})

		if !strings.Contains(output, "\"app\"") {
			t.Fatalf("expected app in output, got %q", output)
		}
		if !strings.Contains(output, "\"entities\"") {
			t.Fatalf("expected entities in output, got %q", output)
		}
		if !strings.Contains(output, "\"relations\"") {
			t.Fatalf("expected relations in output, got %q", output)
		}
		if !strings.Contains(output, "expense_report") {
			t.Fatalf("expected entity name in output, got %q", output)
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ServerURL = "http://unused"
		if err := runSchema("myapp", "default"); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails on API error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 500, "msg": "server error"})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		if err := runSchema("myapp", "default"); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails on unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = "http://unused"
		if err := runSchema("myapp", "nonexistent"); err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})
}
