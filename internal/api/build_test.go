/**
 * [INPUT]: 依赖 api 包内的 Client.GetBuildTask（包内白盒），encoding/json、errors、net/http、net/http/httptest、testing
 * [OUTPUT]: 覆盖构建任务查询的单元测试（请求形态 / 字段解析 / 404 与软空响应的 ErrNotFound 收敛 / 业务错误不映射 not-found / 传输错误）+ Finished/Succeeded 终态判定表测
 * [POS]: internal/api 模块 build.go 的配套测试，用 httptest 隔离网络
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestBuildTaskStates 覆盖终态判定：Finished 对三个终态为真、进行态为假，Succeeded 只认 SUCCESS。
func TestBuildTaskStates(t *testing.T) {
	cases := []struct {
		status    string
		finished  bool
		succeeded bool
	}{
		{BuildStatusSuccess, true, true},
		{BuildStatusFailed, true, false},
		{BuildStatusCanceled, true, false},
		{"PENDING", false, false},
		{"RUNNING", false, false},
		{"", false, false},
	}
	for _, c := range cases {
		task := &BuildTask{Status: c.status}
		if got := task.Finished(); got != c.finished {
			t.Errorf("Finished(%q) = %v, want %v", c.status, got, c.finished)
		}
		if got := task.Succeeded(); got != c.succeeded {
			t.Errorf("Succeeded(%q) = %v, want %v", c.status, got, c.succeeded)
		}
	}
}

func TestGetBuildTask(t *testing.T) {
	t.Run("sends correct request and parses task", func(t *testing.T) {
		var gotTarget, gotPath string
		var gotBody map[string]any
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotTarget = r.Header.Get("X-Make-Target")
			gotPath = r.URL.Path
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"code": 200, "msg": "success",
				"data": {
					"id": 582, "orgId": 87, "appKey": "myapp",
					"deploymentVersion": "deploy_myapp_20260722_004",
					"environment": "preview",
					"repoOwner": "87", "repoName": "myapp-preview",
					"ref": "refs/heads/dev",
					"commitSha": "7cf75e170d13adf406bd3bb98f386ce62984198e",
					"commitMessage": "feat: add canvas copy toast",
					"status": "SUCCESS", "phase": "PUSH",
					"image": "repo.example/87/myapp-preview-service:20260722063526-582",
					"createTime": "2026-07-22T14:35:24+08:00",
					"updateTime": "2026-07-22T15:45:50+08:00",
					"startTime": "2026-07-22T14:35:25+08:00",
					"finishTime": "2026-07-22T15:45:04+08:00"
				}
			}`))
		}))
		defer srv.Close()

		task, err := New(srv.URL, "test-token").GetBuildTask("7cf75e170d13adf406bd3bb98f386ce62984198e")
		if err != nil {
			t.Fatalf("GetBuildTask: %v", err)
		}
		if gotTarget != "MakeService.GetResource" {
			t.Errorf("X-Make-Target = %q, want MakeService.GetResource", gotTarget)
		}
		if gotPath != "/build/v1/build" {
			t.Errorf("path = %q, want /build/v1/build", gotPath)
		}
		if gotBody["commitSha"] != "7cf75e170d13adf406bd3bb98f386ce62984198e" {
			t.Errorf("unexpected request body: %v", gotBody)
		}
		if task.ID != 582 || task.AppKey != "myapp" || task.Environment != "preview" {
			t.Errorf("task identity mismatch: %+v", task)
		}
		if task.Status != "SUCCESS" || task.Phase != "PUSH" {
			t.Errorf("status/phase = %q/%q, want SUCCESS/PUSH", task.Status, task.Phase)
		}
		if task.CommitMessage != "feat: add canvas copy toast" || task.Image == "" {
			t.Errorf("commit/image mismatch: %+v", task)
		}
		if task.StartTime != "2026-07-22T14:35:25+08:00" || task.FinishTime != "2026-07-22T15:45:04+08:00" {
			t.Errorf("time fields mismatch: %+v", task)
		}
	})

	t.Run("returns ErrNotFound on not-found business code", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code": 404, "msg": "build task not found"}`))
		}))
		defer srv.Close()

		_, err := New(srv.URL, "t").GetBuildTask("deadbeef")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})

	t.Run("returns ErrNotFound on soft-empty response", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code": 200, "msg": "success", "data": {}}`))
		}))
		defer srv.Close()

		_, err := New(srv.URL, "t").GetBuildTask("deadbeef")
		if !errors.Is(err, ErrNotFound) {
			t.Fatalf("expected ErrNotFound on empty data, got %v", err)
		}
	})

	t.Run("server error is not mapped to not-found", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"code": 500, "msg": "boom"}`))
		}))
		defer srv.Close()

		_, err := New(srv.URL, "t").GetBuildTask("deadbeef")
		if err == nil {
			t.Fatal("expected error on code 500")
		}
		if errors.Is(err, ErrNotFound) {
			t.Fatalf("server error must not be ErrNotFound, got %v", err)
		}
	})

	t.Run("fails on transport error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {}))
		srv.Close() // 立刻关掉制造连接失败

		if _, err := New(srv.URL, "t").GetBuildTask("deadbeef"); err == nil {
			t.Fatal("expected transport error")
		}
	})
}
