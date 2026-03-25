/**
 * [INPUT]: 依赖 cmd 包内的 runRecordGet（包内白盒），internal/config、encoding/json、net/http、net/http/httptest
 * [OUTPUT]: 覆盖 record get 子命令核心逻辑的单元测试（成功/JSON输出/无凭证/API错误/未知profile/非法格式）
 * [POS]: cmd 模块 record_get.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
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

func TestRunRecordGet(t *testing.T) {
	t.Run("gets record successfully", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Make-Target") != "MakeService.GetResource" {
				t.Errorf("unexpected X-Make-Target: %s", r.Header.Get("X-Make-Target"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "success",
				"data": map[string]any{
					"recordID": "rec_001",
					"name":     "张三",
					"age":      18,
				},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		out := captureStdout(t, func() {
			if err := runRecordGet("TODO", "用户", "rec_001", "default", outputTable); err != nil {
				t.Fatalf("runRecordGet: %v", err)
			}
		})

		if !strings.Contains(out, "张三") {
			t.Fatalf("expected name in output, got %q", out)
		}
		if !strings.Contains(out, "rec_001") {
			t.Fatalf("expected recordID in output, got %q", out)
		}
	})

	t.Run("gets record as json", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "success",
				"data": map[string]any{
					"recordID": "rec_001",
					"name":     "张三",
					"age":      18,
				},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		out := captureStdout(t, func() {
			if err := runRecordGet("TODO", "用户", "rec_001", "default", outputJSON); err != nil {
				t.Fatalf("runRecordGet json: %v", err)
			}
		})

		if !strings.Contains(out, "\"name\"") {
			t.Fatalf("expected name key in JSON output, got %q", out)
		}
		if !strings.Contains(out, "\"data\"") {
			t.Fatalf("expected data key in JSON output, got %q", out)
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ServerURL = "http://unused"
		if err := runRecordGet("TODO", "用户", "rec_001", "default", outputTable); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails on API error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 404, "msg": "record not found"})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		if err := runRecordGet("TODO", "用户", "rec_001", "default", outputTable); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = "http://unused"
		if err := runRecordGet("TODO", "用户", "rec_001", "nonexistent", outputTable); err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})

	t.Run("fails on unsupported output format", func(t *testing.T) {
		if err := runRecordGet("TODO", "用户", "rec_001", "default", "xml"); err == nil {
			t.Fatal("expected error for unsupported output format")
		}
	})
}
