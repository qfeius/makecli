package cmd

import (
	"context"
	"io"

	"bytes"
	"encoding/json"
	"fmt"
	"github.com/qfeius/makecli/internal/api"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLogsRuntimeQueriesTraceWithoutAppOrTimeAndPaginates(t *testing.T) {
	const traceID = "9615209586ba957b3423f1023fd2d601"
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/api/make/console/v1/log" || r.URL.RawQuery != "" || r.Header.Get("X-Make-Target") != "MakeService.ListResources" || r.Header.Get("X-Make-Source") != "cli" || r.Header.Get("X-Make-App-Key") != "" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["traceId"] != nil || body["appKey"] != nil || body["timeRange"] != nil || body["sortOrder"] != "asc" {
			t.Errorf("unexpected body: %v", body)
		}
		filter := body["filter"].(map[string]any)
		if filter["expression"] != "trace_id = '"+traceID+"' AND event_type = 'make_runtime'" {
			t.Errorf("unexpected filter: %v", filter)
		}
		page := int(body["pagination"].(map[string]any)["page"].(float64))
		if page != calls {
			t.Errorf("page = %d, want %d", page, calls)
		}
		rows := make([]map[string]any, 0, 100)
		count := 100
		if page == 2 {
			count = 1
		}
		for i := 0; i < count; i++ {
			rows = append(rows, map[string]any{"traceId": traceID, "serviceName": "make-meta-service", "occurredAt": "2026-11-18T11:00:00+08:00", "level": "info", "content": map[string]any{"message": fmt.Sprintf("message-%d-%d", page, i)}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": rows, "pagination": map[string]int{"page": page, "size": 100, "total": 101}})
	}))
	defer server.Close()
	root := agentCreateTestRoot(t, server.URL)
	root.AddCommand(newLogsCmd())
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"logs", "runtime", "--trace-id", traceID, "--output", "json"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
	var result struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v: %s", err, out.String())
	}
	if len(result.Data) != 101 {
		t.Fatalf("got %d logs", len(result.Data))
	}
}

func TestLogsRuntimeRejectsInvalidTraceWithoutRequest(t *testing.T) {
	for _, id := range []string{"", "not-a-trace", "00000000000000000000000000000000"} {
		cmd := newLogsRuntimeCmd()
		cmd.SetArgs([]string{"--trace-id", id})
		if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "trace") {
			t.Fatalf("trace %q: expected error, got %v", id, err)
		}
	}
}

func TestRuntimeLogsReportsPartialResults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":200,"data":[{"content":{"message":"first"}}],"pagination":{"page":1,"size":100,"total":200}}`))
	}))
	defer server.Close()
	var out bytes.Buffer
	if err := readRuntimeLogs(context.Background(), api.New(server.URL, "token"), "9615209586ba957b3423f1023fd2d601", outputJSON, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Total     int  `json:"total"`
		Truncated bool `json:"truncated"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Truncated || got.Total != 200 {
		t.Fatalf("partial result not reported: %+v", got)
	}
}

func TestRuntimeLogsReportsServerFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":400,"msg":"appKey不能为空"}`))
	}))
	defer server.Close()
	var out bytes.Buffer
	err := readRuntimeLogs(context.Background(), api.New(server.URL, "token"), "9615209586ba957b3423f1023fd2d601", outputJSON, &out, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "appKey不能为空") || out.Len() != 0 {
		t.Fatalf("failure not propagated: %v / %s", err, out.String())
	}
}

func TestRuntimeLogsTableIncludesStackTrace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":200,"data":[{"occurredAt":"2026-11-18T11:00:00+08:00","serviceName":"make-meta-service","level":"error","content":{"message":"create entity failed"},"stackTrace":"java.lang.IllegalStateException: failed\n\tat example.EntityService.create(EntityService.java:42)"}],"pagination":{"page":1,"size":100,"total":1}}`))
	}))
	defer server.Close()
	var out bytes.Buffer
	if err := readRuntimeLogs(context.Background(), api.New(server.URL, "token"), "9615209586ba957b3423f1023fd2d601", outputTable, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"create entity failed", "java.lang.IllegalStateException: failed", "example.EntityService.create"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("missing %q in output: %s", want, out.String())
		}
	}
}
