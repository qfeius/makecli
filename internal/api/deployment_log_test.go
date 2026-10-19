/**
 * [INPUT]: Deployment event API, httptest and canceled contexts
 * [OUTPUT]: Event cursor, tenant/task identity, errors and cancellation contracts
 * [POS]: Deployment process log client tests
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDeploymentLogRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if r.URL.Path != "/deployment/v1/deployment/events" || r.Header.Get("X-Make-Target") != "MakeService.GetResource" || r.Header.Get("Authorization") != "Bearer token" || r.Header.Get("Traceparent") == "" {
			t.Errorf("request: %s %v", r.URL, r.Header)
		}
		if body["tenantID"] != "1001" || body["appKey"] != "test_beta" || body["buildTaskId"] != "42" || body["cursor"] != float64(30) {
			t.Errorf("body=%v", body)
		}
		_, _ = io.WriteString(w, `{"code":200,"data":{"content":"ready\n","nextCursor":400,"completed":true}}`)
	}))
	defer srv.Close()
	chunk, err := New(srv.URL, "token").GetDeploymentLog(context.Background(), BuildTask{ID: 42, OrgID: 1001, AppKey: "test_beta"}, 30)
	if err != nil || chunk.Content != "ready\n" || chunk.NextCursor != 400 || !chunk.Completed {
		t.Fatalf("chunk=%+v err=%v", chunk, err)
	}
}

func TestDeploymentLogErrors(t *testing.T) {
	for _, body := range []string{`{"code":200}`, `{"code":500,"msg":"storage failed"}`, `{"code":990300403,"msg":"invalid token"}`, `{"code":200,"data":{"nextCursor":-1}}`, `not json`} {
		t.Run(body, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }))
			defer srv.Close()
			_, err := New(srv.URL, "token").GetDeploymentLog(context.Background(), BuildTask{ID: 42, OrgID: 1001, AppKey: "test_beta"}, 0)
			if err == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
}

func TestDeploymentLogCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { _, _ = io.Copy(io.Discard, r.Body); <-r.Context().Done() }))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := New(srv.URL, "token").GetDeploymentLog(ctx, BuildTask{ID: 42, OrgID: 1001, AppKey: "test_beta"}, 0)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
}

func TestBuildTaskByIDRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["id"] != "42" || body["commitSha"] != nil {
			t.Errorf("task not pinned: %v", body)
		}
		_, _ = io.WriteString(w, `{"code":200,"data":{"id":42,"orgId":1001,"appKey":"test_beta","status":"FAILED","phase":"BUILD"}}`)
	}))
	defer srv.Close()
	task, err := New(srv.URL, "token").GetBuildTaskByIDContext(context.Background(), 42)
	if err != nil || task.OrgID != 1001 || !task.Finished() {
		t.Fatalf("task=%+v err=%v", task, err)
	}
}

func TestBuildTaskRoutingIdentityNotOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"code":200,"data":{"id":42,"orgId":1001,"appKey":"test_beta"}}`)
	}))
	defer srv.Close()
	task, err := New(srv.URL, "token").GetBuildTaskContext(context.Background(), "sha", EnvBeta)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(task)
	if err != nil {
		t.Fatal(err)
	}
	var view map[string]any
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	if _, exists := view["orgId"]; exists {
		t.Fatal("internal log routing identity changed app deploy JSON output")
	}
	if task.OrgID != 1001 {
		t.Fatal("routing identity lost")
	}
}
