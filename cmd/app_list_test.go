/**
 * [INPUT]: 依赖 cmd 包内的 newAppCmd/runAppList/parseFilter，encoding/json、net/http、net/http/httptest
 * [OUTPUT]: 覆盖 app list 与 +favo 的列表请求、分页输出、过滤与错误；验证快捷命令与显式筛选等价
 * [POS]: cmd 模块 app_list.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package cmd

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunAppList(t *testing.T) {
	t.Run("lists apps successfully", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Make-Target") != "MakeService.ListResources" {
				t.Errorf("unexpected X-Make-Target: %s", r.Header.Get("X-Make-Target"))
			}
			var req struct {
				Pagination struct {
					Page int `json:"page"`
					Size int `json:"size"`
				} `json:"pagination"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if req.Pagination.Page != 1 {
				t.Errorf("unexpected pagination page: %d", req.Pagination.Page)
			}
			if req.Pagination.Size != 20 {
				t.Errorf("unexpected pagination size: %d", req.Pagination.Size)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "message": "success",
				"data": []map[string]any{
					{"key": "ProjectA", "name": "项目A", "type": "Make.App",
						"meta":       map[string]any{"version": "1.0.0", "createdAt": "2026-04-03T04:44:23Z"},
						"properties": map[string]any{"description": "demo"}},
				},
				"pagination": map[string]any{"page": 1, "size": 20, "total": 1},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		if err := runAppList(1, 20, outputTable, ""); err != nil {
			t.Fatalf("runAppList: %v", err)
		}
	})

	t.Run("empty list prints message", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "message": "success",
				"data":       []any{},
				"pagination": map[string]any{"page": 1, "size": 20, "total": 0},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		if err := runAppList(1, 20, outputTable, ""); err != nil {
			t.Fatalf("runAppList: %v", err)
		}
	})

	t.Run("prints json when requested", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "message": "success",
				"data": []map[string]any{
					{"key": "ProjectA", "name": "项目A", "type": "Make.App",
						"meta":       map[string]any{"version": "1.0.0", "createdAt": "2026-04-03T04:44:23Z"},
						"properties": map[string]any{"description": "demo"}},
				},
				"pagination": map[string]any{"page": 1, "size": 20, "total": 1},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		output := captureStdout(t, func() {
			if err := runAppList(2, 20, outputJSON, ""); err != nil {
				t.Fatalf("runAppList json: %v", err)
			}
		})

		if !strings.Contains(output, "\"data\"") {
			t.Fatalf("expected JSON output, got %q", output)
		}
		if !strings.Contains(output, "\"count\": 1") {
			t.Fatalf("expected pagination count in JSON output, got %q", output)
		}
		if !strings.Contains(output, "\"page\": 2") {
			t.Fatalf("expected pagination page in JSON output, got %q", output)
		}
		if strings.Contains(output, "Showing 1 of 1 apps") {
			t.Fatalf("expected JSON-only output, got %q", output)
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		MetaServerURL = "http://unused"
		if err := runAppList(1, 20, outputTable, ""); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails on API error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 500, "message": "server error"})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		if err := runAppList(1, 20, outputTable, ""); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("fails when page is less than 1", func(t *testing.T) {
		if err := runAppList(0, 20, outputTable, ""); err == nil {
			t.Fatal("expected error for invalid page")
		}
	})

	t.Run("fails when size is less than 1", func(t *testing.T) {
		if err := runAppList(1, 0, outputTable, ""); err == nil {
			t.Fatal("expected error for invalid size")
		}
	})

	t.Run("fails on unsupported output format", func(t *testing.T) {
		if err := runAppList(1, 20, "xml", ""); err == nil {
			t.Fatal("expected error for unsupported output format")
		}
	})

	t.Run("sends filter to API", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var req map[string]any
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			obj, ok := req["filter"].(map[string]any)
			if !ok {
				t.Fatalf("expected filter to be Expression object, got %T", req["filter"])
			}
			want := "name.contains('todo') || key == 'todo'"
			if obj["expression"] != want {
				t.Errorf("expected expression %q, got %v", want, obj["expression"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "message": "success",
				"data":       []any{},
				"pagination": map[string]any{"page": 1, "size": 20, "total": 0},
			})
		}))
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		if err := runAppList(1, 20, outputTable, "name=todo,key=todo"); err != nil {
			t.Fatalf("runAppList with filter: %v", err)
		}
	})

	t.Run("fails on invalid filter expression", func(t *testing.T) {
		if err := runAppList(1, 20, outputTable, "badfilter"); err == nil {
			t.Fatal("expected error for invalid filter")
		}
	})

	t.Run("fails on unsupported filter field", func(t *testing.T) {
		if err := runAppList(1, 20, outputTable, "status=active"); err == nil {
			t.Fatal("expected error for unsupported filter field")
		}
	})
}

func TestParseFilter(t *testing.T) {
	t.Run("workbench group becomes equality CEL", func(t *testing.T) {
		f, err := parseFilter("workbenchGroup=favorite")
		if err != nil {
			t.Fatal(err)
		}
		if f != "workbenchGroup == 'favorite'" {
			t.Fatalf("unexpected filter: %q", f)
		}
	})
	t.Run("empty returns empty string", func(t *testing.T) {
		f, err := parseFilter("")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f != "" {
			t.Fatalf("expected empty string, got %q", f)
		}
	})

	t.Run("name field becomes contains CEL", func(t *testing.T) {
		f, err := parseFilter("name=项目")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f != "name.contains('项目')" {
			t.Errorf("expected name.contains('项目'), got %q", f)
		}
	})

	t.Run("key field becomes equality CEL", func(t *testing.T) {
		f, err := parseFilter("key=todo")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f != "key == 'todo'" {
			t.Errorf("expected key == 'todo', got %q", f)
		}
	})

	t.Run("comma separated fields join with OR", func(t *testing.T) {
		f, err := parseFilter("name=todo,key=todo")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f != "name.contains('todo') || key == 'todo'" {
			t.Errorf("expected OR-joined CEL, got %q", f)
		}
	})

	t.Run("escapes single quote in value", func(t *testing.T) {
		f, err := parseFilter("name=it's")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f != `name.contains('it\'s')` {
			t.Errorf("expected escaped quote, got %q", f)
		}
	})

	t.Run("rejects missing value", func(t *testing.T) {
		if _, err := parseFilter("name="); err == nil {
			t.Fatal("expected error for missing value")
		}
	})

	t.Run("rejects missing key", func(t *testing.T) {
		if _, err := parseFilter("=value"); err == nil {
			t.Fatal("expected error for missing key")
		}
	})

	t.Run("rejects unknown field", func(t *testing.T) {
		if _, err := parseFilter("foo=bar"); err == nil {
			t.Fatal("expected error for unknown field")
		}
	})
}

func TestAppFavoMatchesListFilter(t *testing.T) {
	t.Setenv("MAKE_CLI_CONFIG_DIR", t.TempDir())
	setProfile(t, "default")
	oldURL, oldToken := MetaServerURL, AccessToken
	t.Cleanup(func() { MetaServerURL, AccessToken = oldURL, oldToken })
	AccessToken = "test-token"
	var requests []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/make/meta/v1/app" || r.Header.Get("X-Make-Target") != "MakeService.ListResources" {
			t.Errorf("unexpected request: %s %s, target=%s", r.Method, r.URL, r.Header.Get("X-Make-Target"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		requests = append(requests, body)
		_, _ = w.Write([]byte(`{"code":200,"msg":"success","data":[{"key":"ProjectA","name":"项目A","meta":{"favorite":true}}],"pagination":{"total":21}}`))
	}))
	defer srv.Close()
	MetaServerURL = srv.URL

	var outputs []string
	for _, args := range [][]string{
		{"+favo", "--page", "2", "--size", "10", "--output", "json"},
		{"list", "--filter", "workbenchGroup=favorite", "--page", "2", "--size", "10", "--output", "json"},
	} {
		cmd := newAppCmd()
		cmd.SetArgs(args)
		outputs = append(outputs, captureStdout(t, func() {
			if err := cmd.Execute(); err != nil {
				t.Error(err)
			}
		}))
	}
	if len(requests) != 2 {
		t.Fatalf("got %d requests, want 2", len(requests))
	}
	for _, body := range requests {
		filter, _ := body["filter"].(map[string]any)
		pagination, _ := body["pagination"].(map[string]any)
		if filter["expression"] != "workbenchGroup == 'favorite'" || pagination["page"] != float64(2) || pagination["size"] != float64(10) {
			t.Errorf("unexpected list request: %+v", body)
		}
	}
	if outputs[0] != outputs[1] || !strings.Contains(outputs[0], `"ProjectA"`) || !strings.Contains(outputs[0], `"total": 21`) {
		t.Fatalf("shortcut and list outputs differ or omit data: %q / %q", outputs[0], outputs[1])
	}
}

func TestAppFavoRejectsOverrides(t *testing.T) {
	for _, args := range [][]string{{"+favo", "ProjectA"}, {"+favo", "--filter", "workbenchGroup=all"}} {
		cmd := newAppCmd()
		cmd.SetArgs(args)
		if err := cmd.Execute(); err == nil {
			t.Fatalf("expected invalid arguments to fail: %v", args)
		}
	}
}
