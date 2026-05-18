/**
 * [INPUT]: 依赖 internal/api 包内的 Client（包内白盒），encoding/json、net/http、net/http/httptest
 * [OUTPUT]: 覆盖 CreateRecord / GetRecord / UpdateRecord / UpdateRecordsBatch / DeleteRecords / ListRecords 的单元测试
 * [POS]: internal/api record.go 的配套测试，用 httptest 隔离网络
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ---------------------------------- CreateRecord ----------------------------------

func TestRecordCreate(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/data/v1/record" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			if r.Header.Get("X-Make-Target") != "MakeService.CreateResource" {
				t.Errorf("unexpected X-Make-Target: %s", r.Header.Get("X-Make-Target"))
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["appKey"] != "myapp" || body["entityKey"] != "user" {
				t.Errorf("unexpected body: %v", body)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "ok",
				"data": map[string]any{"recordID": "rec-001"},
			})
		}))
		defer srv.Close()

		id, err := New(srv.URL, "test-token").CreateRecord("myapp", "user", map[string]any{"name": "Jim"})
		if err != nil {
			t.Fatalf("CreateRecord: %v", err)
		}
		if id != "rec-001" {
			t.Errorf("expected recordID=rec-001, got %s", id)
		}
	})

	t.Run("API error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 400, "msg": "invalid entity"})
		}))
		defer srv.Close()

		if _, err := New(srv.URL, "test-token").CreateRecord("myapp", "bad", nil); err == nil {
			t.Fatal("expected error on API failure")
		}
	})
}

// ---------------------------------- GetRecord ----------------------------------

func TestRecordGet(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/data/v1/record" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			if r.Header.Get("X-Make-Target") != "MakeService.GetResource" {
				t.Errorf("unexpected X-Make-Target: %s", r.Header.Get("X-Make-Target"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "ok",
				"data": map[string]any{"name": "Jim", "age": float64(30)},
			})
		}))
		defer srv.Close()

		data, err := New(srv.URL, "test-token").GetRecord("myapp", "user", "rec-001")
		if err != nil {
			t.Fatalf("GetRecord: %v", err)
		}
		if data["name"] != "Jim" {
			t.Errorf("unexpected name: %v", data["name"])
		}
	})

	t.Run("API error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 404, "msg": "not found"})
		}))
		defer srv.Close()

		if _, err := New(srv.URL, "test-token").GetRecord("myapp", "user", "bad"); err == nil {
			t.Fatal("expected error on API failure")
		}
	})
}

// ---------------------------------- UpdateRecord ----------------------------------

func TestRecordUpdate(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/data/v1/record" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			if r.Header.Get("X-Make-Target") != "MakeService.UpdateResource" {
				t.Errorf("unexpected X-Make-Target: %s", r.Header.Get("X-Make-Target"))
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["recordID"] != "rec-001" {
				t.Errorf("unexpected recordID: %v", body["recordID"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "ok"})
		}))
		defer srv.Close()

		err := New(srv.URL, "test-token").UpdateRecord("myapp", "user", "rec-001", map[string]any{"name": "Yu"})
		if err != nil {
			t.Fatalf("UpdateRecord: %v", err)
		}
	})

	t.Run("API error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 500, "msg": "internal error"})
		}))
		defer srv.Close()

		if err := New(srv.URL, "test-token").UpdateRecord("myapp", "user", "rec-001", nil); err == nil {
			t.Fatal("expected error on API failure")
		}
	})
}

// ---------------------------------- UpdateRecordsBatch ----------------------------------

func TestRecordUpdateBatch(t *testing.T) {
	t.Run("success with /data/v1/field path", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// 核心断言：批量更新走 /data/v1/field 而非 /data/v1/record
			if r.URL.Path != "/data/v1/field" {
				t.Errorf("expected path /data/v1/field, got %s", r.URL.Path)
			}
			if r.Header.Get("X-Make-Target") != "MakeService.UpdateResource" {
				t.Errorf("unexpected X-Make-Target: %s", r.Header.Get("X-Make-Target"))
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			ids, ok := body["recordIDList"].([]any)
			if !ok || len(ids) != 2 {
				t.Errorf("expected recordIDList with 2 items, got %v", body["recordIDList"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "ok"})
		}))
		defer srv.Close()

		err := New(srv.URL, "test-token").UpdateRecordsBatch(
			"myapp", "user",
			[]string{"rec-001", "rec-002"},
			map[string]any{"status": "active"},
		)
		if err != nil {
			t.Fatalf("UpdateRecordsBatch: %v", err)
		}
	})

	t.Run("API error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 400, "msg": "bad request"})
		}))
		defer srv.Close()

		err := New(srv.URL, "test-token").UpdateRecordsBatch("myapp", "user", []string{"x"}, nil)
		if err == nil {
			t.Fatal("expected error on API failure")
		}
	})
}

// ---------------------------------- DeleteRecords ----------------------------------

func TestRecordDelete(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/data/v1/record" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			if r.Header.Get("X-Make-Target") != "MakeService.DeleteResource" {
				t.Errorf("unexpected X-Make-Target: %s", r.Header.Get("X-Make-Target"))
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			ids, ok := body["recordIDList"].([]any)
			if !ok || len(ids) != 2 {
				t.Errorf("expected recordIDList with 2 items, got %v", body["recordIDList"])
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "ok",
				"data": []map[string]any{
					{"recordID": "rec-001", "code": 200, "msg": "deleted"},
					{"recordID": "rec-002", "code": 200, "msg": "deleted"},
				},
			})
		}))
		defer srv.Close()

		results, err := New(srv.URL, "test-token").DeleteRecords("myapp", "user", []string{"rec-001", "rec-002"})
		if err != nil {
			t.Fatalf("DeleteRecords: %v", err)
		}
		if len(results) != 2 {
			t.Errorf("expected 2 results, got %d", len(results))
		}
		if results[0].RecordID != "rec-001" || results[0].Code != 200 {
			t.Errorf("unexpected first result: %+v", results[0])
		}
	})

	t.Run("API error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 500, "msg": "internal error"})
		}))
		defer srv.Close()

		if _, err := New(srv.URL, "test-token").DeleteRecords("myapp", "user", []string{"x"}); err == nil {
			t.Fatal("expected error on API failure")
		}
	})
}

// ---------------------------------- ListRecords ----------------------------------

func TestRecordList(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/data/v1/record" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			if r.Header.Get("X-Make-Target") != "MakeService.ListResources" {
				t.Errorf("unexpected X-Make-Target: %s", r.Header.Get("X-Make-Target"))
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["appKey"] != "myapp" || body["entityKey"] != "user" {
				t.Errorf("unexpected body: %v", body)
			}
			// 验证可选字段被发送
			if body["fields"] == nil {
				t.Error("expected fields in request body")
			}
			if body["sort"] == nil {
				t.Error("expected sort in request body")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "ok",
				"data": []map[string]any{
					{"name": "Jim", "age": 30},
					{"name": "Yu", "age": 25},
				},
				"pagination": map[string]any{"total": 50},
			})
		}))
		defer srv.Close()

		records, total, err := New(srv.URL, "test-token").ListRecords("myapp", "user", ListRecordOpts{
			Fields: []string{"name", "age"},
			Sort:   []SortField{{FieldKey: "age", Order: "desc"}},
			Page:   1,
			Size:   10,
		})
		if err != nil {
			t.Fatalf("ListRecords: %v", err)
		}
		if total != 50 {
			t.Errorf("expected total=50, got %d", total)
		}
		if len(records) != 2 {
			t.Errorf("expected 2 records, got %d", len(records))
		}
		if records[0]["name"] != "Jim" {
			t.Errorf("unexpected first record name: %v", records[0]["name"])
		}
	})

	t.Run("without optional fields", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			// 验证可选字段不被发送
			if body["fields"] != nil {
				t.Error("expected no fields in request body")
			}
			if body["sort"] != nil {
				t.Error("expected no sort in request body")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200, "msg": "ok",
				"data":       []map[string]any{},
				"pagination": map[string]any{"total": 0},
			})
		}))
		defer srv.Close()

		records, total, err := New(srv.URL, "test-token").ListRecords("myapp", "user", ListRecordOpts{
			Page: 1, Size: 10,
		})
		if err != nil {
			t.Fatalf("ListRecords: %v", err)
		}
		if total != 0 || len(records) != 0 {
			t.Errorf("expected empty result, got records=%d total=%d", len(records), total)
		}
	})

	t.Run("API error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 500, "msg": "internal error"})
		}))
		defer srv.Close()

		if _, _, err := New(srv.URL, "test-token").ListRecords("myapp", "user", ListRecordOpts{Page: 1, Size: 10}); err == nil {
			t.Fatal("expected error on API failure")
		}
	})
}
