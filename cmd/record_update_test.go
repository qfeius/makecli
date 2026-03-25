/**
 * [INPUT]: 依赖 cmd 包内的 runRecordUpdate / loadRecordData（包内白盒），internal/config、encoding/json、net/http、net/http/httptest、os、strings、testing
 * [OUTPUT]: 覆盖 record update 子命令核心逻辑的单元测试，重点验证单条/批量路由选择
 * [POS]: cmd 模块 record_update.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
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
)

func TestRunRecordUpdate(t *testing.T) {
	t.Run("updates single record successfully", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/data/v1/record" {
				t.Errorf("expected path /data/v1/record, got %s", r.URL.Path)
			}
			if got := r.Header.Get("X-Make-Target"); got != "MakeService.UpdateResource" {
				t.Errorf("expected X-Make-Target MakeService.UpdateResource, got %s", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "success"})
		}))
		defer srv.Close()

		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		jsonFile := writeRecordJSON(t, map[string]any{"status": "done"})

		out := captureStdout(t, func() {
			if err := runRecordUpdate("myapp", "tasks", []string{"rec-001"}, jsonFile, "default"); err != nil {
				t.Fatalf("runRecordUpdate single: %v", err)
			}
		})
		if !strings.Contains(out, "rec-001") {
			t.Errorf("expected output to contain record ID, got: %s", out)
		}
	})

	t.Run("updates multiple records in batch", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/data/v1/field" {
				t.Errorf("expected path /data/v1/field, got %s", r.URL.Path)
			}
			if got := r.Header.Get("X-Make-Target"); got != "MakeService.UpdateResource" {
				t.Errorf("expected X-Make-Target MakeService.UpdateResource, got %s", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "success"})
		}))
		defer srv.Close()

		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		jsonFile := writeRecordJSON(t, map[string]any{"status": "archived"})

		out := captureStdout(t, func() {
			ids := []string{"rec-001", "rec-002", "rec-003"}
			if err := runRecordUpdate("myapp", "tasks", ids, jsonFile, "default"); err != nil {
				t.Fatalf("runRecordUpdate batch: %v", err)
			}
		})
		if !strings.Contains(out, "3 records updated") {
			t.Errorf("expected '3 records updated' in output, got: %s", out)
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ServerURL = "http://unused"

		jsonFile := writeRecordJSON(t, map[string]any{"status": "done"})

		if err := runRecordUpdate("myapp", "tasks", []string{"rec-001"}, jsonFile, "default"); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails on API error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 400, "msg": "bad request"})
		}))
		defer srv.Close()

		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		jsonFile := writeRecordJSON(t, map[string]any{"status": "done"})

		if err := runRecordUpdate("myapp", "tasks", []string{"rec-001"}, jsonFile, "default"); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = "http://unused"

		jsonFile := writeRecordJSON(t, map[string]any{"status": "done"})

		if err := runRecordUpdate("myapp", "tasks", []string{"rec-001"}, jsonFile, "nonexistent"); err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})

	t.Run("fails with invalid JSON file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = "http://unused"

		bad := filepath.Join(t.TempDir(), "bad.json")
		_ = os.WriteFile(bad, []byte("not json"), 0644)

		if err := runRecordUpdate("myapp", "tasks", []string{"rec-001"}, bad, "default"); err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})
}
