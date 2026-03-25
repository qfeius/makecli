/**
 * [INPUT]: 依赖 cmd 包内的 runRecordDelete（包内白盒），internal/config、testing、net/http/httptest
 * [OUTPUT]: 覆盖 record delete 子命令核心逻辑的单元测试
 * [POS]: cmd 模块 record_delete.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
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

// newMockDeleteRecords 创建返回批量删除结果的 mock server
func newMockDeleteRecords(t *testing.T, code int, msg string, data []map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": code,
			"msg":  msg,
			"data": data,
		})
	}))
}

func TestRunRecordDelete(t *testing.T) {
	t.Run("deletes single record successfully", func(t *testing.T) {
		srv := newMockDeleteRecords(t, 200, "success", []map[string]any{
			{"recordID": "rec_001", "code": 200, "msg": "ok"},
		})
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		if err := runRecordDelete("myapp", "tasks", []string{"rec_001"}, "default"); err != nil {
			t.Fatalf("runRecordDelete: %v", err)
		}
	})

	t.Run("deletes multiple records", func(t *testing.T) {
		srv := newMockDeleteRecords(t, 200, "success", []map[string]any{
			{"recordID": "rec_001", "code": 200, "msg": "ok"},
			{"recordID": "rec_002", "code": 200, "msg": "ok"},
		})
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		out := captureStdout(t, func() {
			if err := runRecordDelete("myapp", "tasks", []string{"rec_001", "rec_002"}, "default"); err != nil {
				t.Fatalf("runRecordDelete: %v", err)
			}
		})

		if !strings.Contains(out, "2 record(s) deleted") {
			t.Fatalf("expected '2 record(s) deleted' in output, got: %s", out)
		}
	})

	t.Run("reports partial failures", func(t *testing.T) {
		srv := newMockDeleteRecords(t, 200, "success", []map[string]any{
			{"recordID": "rec_001", "code": 200, "msg": "ok"},
			{"recordID": "rec_002", "code": 400, "msg": "Permission denied"},
		})
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		var err error
		out := captureStdout(t, func() {
			err = runRecordDelete("myapp", "tasks", []string{"rec_001", "rec_002"}, "default")
		})

		if err == nil {
			t.Fatal("expected error for partial failure")
		}
		if !strings.Contains(err.Error(), "1 of 2") {
			t.Fatalf("expected '1 of 2' in error, got: %v", err)
		}
		if !strings.Contains(out, "FAIL") {
			t.Fatalf("expected 'FAIL' in output, got: %s", out)
		}
		if !strings.Contains(out, "Permission denied") {
			t.Fatalf("expected 'Permission denied' in output, got: %s", out)
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		ServerURL = "http://unused"

		if err := runRecordDelete("myapp", "tasks", []string{"rec_001"}, "default"); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails on API error", func(t *testing.T) {
		srv := newMockDeleteRecords(t, 500, "internal server error", nil)
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = srv.URL

		if err := runRecordDelete("myapp", "tasks", []string{"rec_001"}, "default"); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		ServerURL = "http://unused"

		if err := runRecordDelete("myapp", "tasks", []string{"rec_001"}, "nonexistent"); err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})
}
