/**
 * [INPUT]: 依赖 cmd 包内的 runRecordList / parseSortSpec（包内白盒），internal/config、encoding/json、net/http、net/http/httptest
 * [OUTPUT]: 覆盖 record list 子命令核心逻辑的单元测试（列表/JSON输出/空列表/无凭证/API错误/未知profile/非法页码/非法格式/非法排序）
 * [POS]: cmd 模块 record_list.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
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

func TestRunRecordList(t *testing.T) {
	t.Run("lists records in table format", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "success",
				"data": []map[string]any{
					{"recordID": "rec_001", "name": "张三", "age": 18},
				},
				"pagination": map[string]any{"page": 1, "size": 20, "total": 1},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		out := captureStdout(t, func() {
			if err := runRecordList("TODO", "User", 1, 20, outputTable, "", "", ""); err != nil {
				t.Fatalf("runRecordList: %v", err)
			}
		})

		if !strings.Contains(out, "张三") {
			t.Fatalf("expected '张三' in output, got %q", out)
		}
		if !strings.Contains(out, "Showing 1 of 1") {
			t.Fatalf("expected 'Showing 1 of 1' in output, got %q", out)
		}
	})

	t.Run("lists records as json", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "success",
				"data": []map[string]any{
					{"recordID": "rec_001", "name": "张三", "age": 18},
				},
				"pagination": map[string]any{"page": 1, "size": 20, "total": 1},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		out := captureStdout(t, func() {
			if err := runRecordList("TODO", "User", 1, 20, outputJSON, "", "", ""); err != nil {
				t.Fatalf("runRecordList json: %v", err)
			}
		})

		if !strings.Contains(out, "\"data\"") {
			t.Fatalf("expected '\"data\"' in JSON output, got %q", out)
		}
		if !strings.Contains(out, "\"count\": 1") {
			t.Fatalf("expected '\"count\": 1' in JSON output, got %q", out)
		}
	})

	t.Run("empty list prints message", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "success",
				"data":       []any{},
				"pagination": map[string]any{"page": 1, "size": 20, "total": 0},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		out := captureStdout(t, func() {
			if err := runRecordList("TODO", "User", 1, 20, outputTable, "", "", ""); err != nil {
				t.Fatalf("runRecordList empty: %v", err)
			}
		})

		if !strings.Contains(out, "No records found") {
			t.Fatalf("expected 'No records found' in output, got %q", out)
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		MetaServerURL = "http://unused"
		if err := runRecordList("TODO", "User", 1, 20, outputTable, "", "", ""); err == nil {
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
		MetaServerURL = srv.URL

		if err := runRecordList("TODO", "User", 1, 20, outputTable, "", "", ""); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = "http://unused"
		setProfile(t, "nonexistent")
		if err := runRecordList("TODO", "User", 1, 20, outputTable, "", "", ""); err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})

	t.Run("fails when page is less than 1", func(t *testing.T) {
		if err := runRecordList("TODO", "User", 0, 20, outputTable, "", "", ""); err == nil {
			t.Fatal("expected error for invalid page")
		}
	})

	t.Run("fails when size is less than 1", func(t *testing.T) {
		if err := runRecordList("TODO", "User", 1, 0, outputTable, "", "", ""); err == nil {
			t.Fatal("expected error for invalid size")
		}
	})

	t.Run("fails on unsupported output format", func(t *testing.T) {
		if err := runRecordList("TODO", "User", 1, 20, "xml", "", "", ""); err == nil {
			t.Fatal("expected error for unsupported output format")
		}
	})

	t.Run("fails on invalid sort spec", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = "http://unused"
		if err := runRecordList("TODO", "User", 1, 20, outputTable, "", "bad", ""); err == nil {
			t.Fatal("expected error for invalid sort spec")
		}
	})

	t.Run("sends filter as Expression object", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			obj, ok := req["filter"].(map[string]any)
			if !ok {
				t.Fatalf("expected filter to be Expression object, got %T", req["filter"])
			}
			if obj["expression"] != "amount >= 100" {
				t.Errorf("expected raw CEL passthrough, got %v", obj["expression"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "success",
				"data":       []any{},
				"pagination": map[string]any{"page": 1, "size": 20, "total": 0},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		out := captureStdout(t, func() {
			if err := runRecordList("TODO", "User", 1, 20, outputTable, "", "", "amount >= 100"); err != nil {
				t.Fatalf("runRecordList with filter: %v", err)
			}
		})
		_ = out
	})
}

func TestParseSortSpec(t *testing.T) {
	t.Run("parses valid spec", func(t *testing.T) {
		result, err := parseSortSpec("createdAt:desc,id:asc")
		if err != nil {
			t.Fatalf("parseSortSpec: %v", err)
		}
		if len(result) != 2 {
			t.Fatalf("expected 2 sort fields, got %d", len(result))
		}
		if result[0].FieldKey != "createdAt" || result[0].Order != "desc" {
			t.Errorf("unexpected first sort field: %+v", result[0])
		}
		if result[1].FieldKey != "id" || result[1].Order != "asc" {
			t.Errorf("unexpected second sort field: %+v", result[1])
		}
	})

	t.Run("rejects missing colon", func(t *testing.T) {
		if _, err := parseSortSpec("bad"); err == nil {
			t.Fatal("expected error for missing colon")
		}
	})

	t.Run("rejects invalid order", func(t *testing.T) {
		if _, err := parseSortSpec("field:up"); err == nil {
			t.Fatal("expected error for invalid order")
		}
	})

	t.Run("normalizes order case", func(t *testing.T) {
		result, err := parseSortSpec("name:DESC")
		if err != nil {
			t.Fatalf("parseSortSpec: %v", err)
		}
		if result[0].Order != "desc" {
			t.Errorf("expected normalized order 'desc', got %q", result[0].Order)
		}
	})
}
