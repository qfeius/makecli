/**
 * [INPUT]: 依赖 cmd 包内的 runRecordCreate / loadRecordData（包内白盒），internal/config、encoding/json、net/http、net/http/httptest、os、testing
 * [OUTPUT]: 覆盖 record create 子命令核心逻辑的单元测试
 * [POS]: cmd 模块 record_create.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
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

func TestRunRecordCreate(t *testing.T) {
	t.Run("creates record successfully", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "success",
				"data": map[string]any{"recordID": "rec_001"},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		jsonFile := writeRecordJSON(t, map[string]any{
			"title": "Test Record",
			"status": "active",
		})

		output := captureStdout(t, func() {
			if err := runRecordCreate("TODO", "Task", jsonFile, "default"); err != nil {
				t.Fatalf("runRecordCreate: %v", err)
			}
		})

		if !strings.Contains(output, "rec_001") {
			t.Fatalf("expected output to contain 'rec_001', got: %s", output)
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ServerURL = "http://unused"

		jsonFile := writeRecordJSON(t, map[string]any{"title": "Test"})

		if err := runRecordCreate("TODO", "Task", jsonFile, "default"); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails on API error response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 400, "msg": "invalid record data",
				"data": map[string]any{},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		jsonFile := writeRecordJSON(t, map[string]any{"title": "Test"})

		if err := runRecordCreate("TODO", "Task", jsonFile, "default"); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = "http://unused"

		jsonFile := writeRecordJSON(t, map[string]any{"title": "Test"})

		if err := runRecordCreate("TODO", "Task", jsonFile, "nonexistent"); err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})

	t.Run("fails with invalid JSON file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = "http://unused"

		bad := filepath.Join(t.TempDir(), "bad.json")
		_ = os.WriteFile(bad, []byte("not json"), 0644)

		if err := runRecordCreate("TODO", "Task", bad, "default"); err == nil {
			t.Fatal("expected error for invalid JSON")
		}
	})

	t.Run("fails with nonexistent JSON file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = "http://unused"

		if err := runRecordCreate("TODO", "Task", "/nonexistent.json", "default"); err == nil {
			t.Fatal("expected error for nonexistent file")
		}
	})
}

// writeRecordJSON 将 record data 写入临时 JSON 文件，返回路径
func writeRecordJSON(t *testing.T, data map[string]any) string {
	t.Helper()
	raw, _ := json.Marshal(data)
	path := filepath.Join(t.TempDir(), "record.json")
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}
	return path
}
