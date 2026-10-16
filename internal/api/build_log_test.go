/**
 * [INPUT]: Build log/context clients, httptest and context cancellation
 * [OUTPUT]: Tail/cursor contracts, empty logs, API failures and in-flight cancellation tests
 * [POS]: Build log HTTP client regression tests
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetBuildLog(t *testing.T) {
	for _, cursor := range []string{"", "17"} {
		t.Run("cursor="+cursor, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if r.Method != http.MethodPost || r.URL.Path != "/build/v1/build-log" || r.Header.Get("X-Make-Target") != "MakeService.GetResource" || r.Header.Get("Authorization") != "Bearer test-token" {
					t.Errorf("unexpected route or authentication: %s %s", r.Method, r.URL)
				}
				if r.Header.Get("Traceparent") == "" || r.Header.Get("X-Log-Id") == "" {
					t.Error("missing trace headers")
				}
				if body["buildTaskId"] != "42" || body["scope"] != "BUILD" {
					t.Errorf("unexpected body: %#v", body)
				}
				value, exists := body["cursor"]
				if cursor == "" && exists || cursor != "" && value != cursor {
					t.Errorf("unexpected cursor: %#v", body)
				}
				_, _ = w.Write([]byte(`{"code":200,"msg":"success","data":{"content":"编译\n","nextCursor":"24","completed":true}}`))
			}))
			defer srv.Close()
			chunk, err := New(srv.URL, "test-token").GetBuildLog(context.Background(), 42, cursor)
			if err != nil {
				t.Fatal(err)
			}
			if chunk.Content != "编译\n" || chunk.NextCursor != "24" || !chunk.Completed {
				t.Errorf("unexpected chunk: %+v", chunk)
			}
		})
	}
}

func TestGetBuildLogResponses(t *testing.T) {
	cases := []struct {
		name, body              string
		wantErr, notFound, auth bool
	}{
		{"empty logs are valid", `{"code":200,"data":{"content":"","nextCursor":"","completed":false}}`, false, false, false},
		{"missing data is invalid", `{"code":200}`, true, false, false},
		{"not found", `{"code":404,"msg":"task not found"}`, true, true, false},
		{"auth failed", `{"code":990300403,"msg":"token invalid"}`, true, false, true},
		{"server error", `{"code":500,"msg":"storage failed"}`, true, false, false},
		{"invalid JSON", `not JSON`, true, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(tc.body)) }))
			defer srv.Close()
			_, err := New(srv.URL, "t").GetBuildLog(context.Background(), 42, "")
			if (err != nil) != tc.wantErr || errors.Is(err, ErrNotFound) != tc.notFound || errors.Is(err, ErrAuthFailed) != tc.auth {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestBuildLogRequestsRespectContext(t *testing.T) {
	for _, operation := range []string{"logs", "task", "app"} {
		t.Run(operation, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				_, _ = io.Copy(io.Discard, r.Body)
				<-r.Context().Done()
			}))
			defer srv.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
			defer cancel()
			client := New(srv.URL, "t")
			var err error
			switch operation {
			case "logs":
				_, err = client.GetBuildLog(ctx, 42, "")
			case "app":
				_, err = client.GetAppContext(ctx, "test_app")
			default:
				_, err = client.GetBuildTaskContext(ctx, "commit", EnvBeta)
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("request was not canceled: %v", err)
			}
		})
	}
}

func TestGetBuildLogTailContract(t *testing.T) {
	for _, n := range []int64{0, 2, 200} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["tailLines"] != float64(n) || body["cursor"] != nil {
					t.Errorf("tail body: %#v", body)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"content": "tail", "nextCursor": "99", "completed": true, "tailLines": n}})
			}))
			defer srv.Close()
			chunk, err := New(srv.URL, "test-token").GetBuildLogWithOptions(context.Background(), 42, BuildLogOptions{TailLines: &n})
			if err != nil || chunk.NextCursor != "99" {
				t.Fatalf("chunk=%+v err=%v", chunk, err)
			}
		})
	}
}

func TestGetBuildLogRejectsIgnoredTail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"code":200,"data":{"content":"unbounded history","nextCursor":"99"}}`))
	}))
	defer srv.Close()
	n := int64(200)
	if _, err := New(srv.URL, "test-token").GetBuildLogWithOptions(context.Background(), 42, BuildLogOptions{TailLines: &n}); err == nil {
		t.Fatal("server silently ignored tail")
	}
}
