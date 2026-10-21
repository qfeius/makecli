/**
 * [INPUT]: 依赖 cmd 包内的 runAppDelete/runAppDeleteFromFile/resolveDeleteRoles/confirmDeleteFunc（包内白盒），internal/api、httptest、time
 * [OUTPUT]: 覆盖 app delete 的角色选择、确认、状态等待、all 顺序、409 接续、临时故障重试、失败、超时和 -f 文件模式
 * [POS]: cmd 模块 app_delete.go 的配套测试，用 httptest 隔离网络、t.Setenv 隔离凭证
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"crypto/x509"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/qfeius/makecli/internal/api"
)

// stubConfirm 临时替换 confirmDeleteFunc，t.Cleanup 自动还原，隔离真实终端交互
func stubConfirm(t *testing.T, err error) {
	t.Helper()
	orig := confirmDeleteFunc
	confirmDeleteFunc = func(string, string) error { return err }
	t.Cleanup(func() { confirmDeleteFunc = orig })
}

func TestRunAppDelete(t *testing.T) {
	t.Run("deletes app via API with --yes", func(t *testing.T) {
		srv := newSuccessDeleteMeta(t)
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		if err := runAppDelete("myapp", api.RoleProd, true, time.Second); err != nil {
			t.Fatalf("runAppDelete: %v", err)
		}
	})

	t.Run("deletes app after confirmation succeeds", func(t *testing.T) {
		stubConfirm(t, nil)
		srv := newSuccessDeleteMeta(t)
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		if err := runAppDelete("myapp", api.RoleProd, false, time.Second); err != nil {
			t.Fatalf("runAppDelete: %v", err)
		}
	})

	t.Run("confirmation refusal stops before API", func(t *testing.T) {
		sentinel := errors.New("declined")
		stubConfirm(t, sentinel)
		// 没有 mock server：确认失败必须在触网前短路
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = "http://unused"

		if err := runAppDelete("myapp", api.RoleProd, false, time.Second); !errors.Is(err, sentinel) {
			t.Fatalf("expected confirmation error, got %v", err)
		}
	})

	t.Run("real confirm gate refuses in non-interactive shell", func(t *testing.T) {
		// 不打桩，走真 confirmDeleteByTypingKey；go test 下 stdin 非 TTY，应直接拒绝
		t.Setenv("HOME", t.TempDir())
		MetaServerURL = "http://unused"
		if err := runAppDelete("myapp", api.RoleProd, false, time.Second); err == nil {
			t.Fatal("expected refusal without --yes in non-interactive shell")
		}
	})

	t.Run("fails without credentials", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		MetaServerURL = "http://unused"
		if err := runAppDelete("myapp", api.RoleProd, true, time.Second); err == nil {
			t.Fatal("expected error for missing credentials")
		}
	})

	t.Run("fails on API error response", func(t *testing.T) {
		srv := newMockMeta(t, 400, "app not found")
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		if err := runAppDelete("myapp", api.RoleProd, true, time.Second); err == nil {
			t.Fatal("expected error on API failure")
		}
	})

	t.Run("409 refusal surfaces the server message verbatim", func(t *testing.T) {
		// prod 仍有 beta 配对时服务端 409 拒删：给用户的就是服务端那句话，不套「唯一性」或环境前缀
		msg := "当前应用存在 Beta 环境，请先删除 Beta 环境，再删除正式环境。"
		srv := newMockMeta(t, 409, msg)
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		if err := runAppDelete("myapp", api.RoleProd, true, time.Second); err == nil || err.Error() != msg {
			t.Fatalf("err = %v, want exactly %q", err, msg)
		}
	})

	t.Run("fails with unknown profile", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = "http://unused"
		setProfile(t, "nonexistent")

		if err := runAppDelete("myapp", api.RoleProd, true, time.Second); err == nil {
			t.Fatal("expected error for unknown profile")
		}
	})
}

// newMockDeleteMeta records deletions and reports their final status without looking up the App.
func newMockDeleteMeta(t *testing.T, deleted *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/make/meta/app/delete-status" {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"state": "SUCCEEDED", "step": "COMPLETED", "stepName": "删除完成"}})
			return
		}
		if target := r.Header.Get("X-Make-Target"); target != "MakeService.DeleteResource" {
			t.Errorf("unexpected X-Make-Target %q", target)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var body struct {
			Key string `json:"key"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		*deleted = append(*deleted, body.Key+"?appRole="+r.URL.Query().Get("appRole"))
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "msg": "ok", "data": true})
	}))
}

func newSuccessDeleteMeta(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/make/meta/app/delete-status" {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"state": "SUCCEEDED", "step": "COMPLETED", "stepName": "删除完成"}})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": true})
	}))
}

func TestResolveDeleteRoles(t *testing.T) {
	tests := []struct {
		name, env string
		want      []string
		wantErr   bool
	}{
		{"prod 只删 prod", api.RoleProd, []string{api.RoleProd}, false},
		{"beta 只删 beta", api.RoleBeta, []string{api.RoleBeta}, false},
		{"all 先 beta 后 prod", appRoleAll, []string{api.RoleBeta, api.RoleProd}, false},
		{"空 appRole 拒绝", "", nil, true},
		{"未知 appRole 拒绝", "preview", nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveDeleteRoles(tt.env)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("envs = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRunAppDeleteAll(t *testing.T) {
	// --appRole ALL（大小写不敏感）按 beta → prod 顺序删两次，确认只问一次
	var deleted []string
	srv := newMockDeleteMeta(t, &deleted)
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	saveDefaultToken(t)
	MetaServerURL = srv.URL

	confirms := 0
	orig := confirmDeleteFunc
	confirmDeleteFunc = func(_, _ string) error { confirms++; return nil }
	t.Cleanup(func() { confirmDeleteFunc = orig })

	if err := runAppDelete("myapp", "ALL", false, time.Second); err != nil {
		t.Fatalf("runAppDelete: %v", err)
	}
	want := []string{"myapp?appRole=beta", "myapp?appRole=prod"}
	if !slices.Equal(deleted, want) || confirms != 1 {
		t.Errorf("deleted %v (want %v), confirms %d (want 1)", deleted, want, confirms)
	}
}

func TestRunAppDeleteBetaTarget(t *testing.T) {
	// --appRole beta：body 仍是用户给的 prod key，服务端按 ?appRole=beta 定位配对 App
	var deleted []string
	srv := newMockDeleteMeta(t, &deleted)
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	saveDefaultToken(t)
	MetaServerURL = srv.URL

	var confirmed string
	orig := confirmDeleteFunc
	confirmDeleteFunc = func(k, _ string) error { confirmed = k; return nil }
	t.Cleanup(func() { confirmDeleteFunc = orig })

	if err := runAppDelete("myapp", api.RoleBeta, false, time.Second); err != nil {
		t.Fatalf("runAppDelete: %v", err)
	}
	if confirmed != "myapp" || !slices.Equal(deleted, []string{"myapp?appRole=beta"}) {
		t.Errorf("confirmed %q (want myapp), deleted %v (want [myapp?appRole=beta])", confirmed, deleted)
	}
}

func TestRunAppDeleteFromFile(t *testing.T) {
	t.Run("deletes app from YAML file", func(t *testing.T) {
		srv := newSuccessDeleteMeta(t)
		defer srv.Close()
		t.Setenv("HOME", t.TempDir())
		saveDefaultToken(t)
		MetaServerURL = srv.URL

		f := filepath.Join(t.TempDir(), "app.yaml")
		writeTestFile(t, f, []byte("key: fileapp\nname: 文件应用\ntype: Make.App\n"))

		if err := runAppDeleteFromFile(f, api.RoleProd, true, time.Second); err != nil {
			t.Fatalf("runAppDeleteFromFile: %v", err)
		}
	})

	t.Run("fails on non-yaml file", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "app.txt")
		writeTestFile(t, f, []byte("name: foo"))

		if err := runAppDeleteFromFile(f, api.RoleProd, true, time.Second); err == nil {
			t.Fatal("expected error for non-yaml file")
		}
	})

	t.Run("fails when no Make.App in file", func(t *testing.T) {
		f := filepath.Join(t.TempDir(), "entity.yaml")
		writeTestFile(t, f, []byte("key: foo\nname: 实体\ntype: Make.Entity\nappKey: bar\n"))

		if err := runAppDeleteFromFile(f, api.RoleProd, true, time.Second); err == nil {
			t.Fatal("expected error for missing Make.App")
		}
	})
}

func TestAppDeleteRoleFlag(t *testing.T) {
	command := newAppDeleteCmd()
	if command.Flags().Lookup("appRole") == nil {
		t.Fatal("app delete must require --appRole")
	}
	if command.Flags().Lookup("env") != nil {
		t.Fatal("app delete should no longer expose --env")
	}
	if command.Flags().Lookup("timeout") == nil {
		t.Fatal("app delete must allow a bounded wait")
	}
}

func TestAppDeleteWaitsForBetaBeforeProd(t *testing.T) {
	var calls []string
	betaChecks := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		role := r.URL.Query().Get("appRole")
		switch r.URL.Path {
		case "/api/make/meta/v1/app":
			calls = append(calls, "delete:"+role)
			if role == "prod" && betaChecks < 2 {
				t.Error("prod deletion started before beta succeeded")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": true})
		case "/api/make/meta/app/delete-status":
			calls = append(calls, "status:"+role)
			state := "SUCCEEDED"
			if role == "beta" {
				betaChecks++
				if betaChecks == 1 {
					state = "RUNNING"
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{
				"state": state, "step": "COMPLETED", "stepName": "删除完成",
			}})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	saveDefaultToken(t)
	MetaServerURL = srv.URL
	oldInterval := appDeletePollInterval
	appDeletePollInterval = time.Millisecond
	t.Cleanup(func() { appDeletePollInterval = oldInterval })

	if err := runAppDelete("myapp", "ALL", true, time.Second); err != nil {
		t.Fatalf("runAppDelete: %v", err)
	}
	want := []string{"delete:beta", "status:beta", "status:beta", "delete:prod", "status:prod"}
	if !slices.Equal(calls, want) {
		t.Errorf("calls = %v, want %v", calls, want)
	}
}

func TestAppDeleteFailedBetaStopsAll(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		if r.URL.Path == "/api/make/meta/v1/app" {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": true})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{
			"state": "FAILED", "step": "ARCHIVE_DATA", "stepName": "归档业务数据", "message": "归档失败",
		}})
	}))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	saveDefaultToken(t)
	MetaServerURL = srv.URL

	err := runAppDelete("myapp", "all", true, time.Second)
	if !errors.Is(err, errAppDeleteFailed) || len(calls) != 2 {
		t.Fatalf("err = %v, calls = %v", err, calls)
	}
	want := "删除 App 失败：myapp（beta）；失败步骤：归档业务数据（ARCHIVE_DATA）；原因：归档失败"
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err, want)
	}
}

func TestAppDeleteResumesRunningTaskAfterConflict(t *testing.T) {
	deletes, checks := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/make/meta/v1/app":
			deletes++
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 409, "msg": "应用正在删除，请查询删除状态"})
		case "/api/make/meta/app/delete-status":
			checks++
			state := "RUNNING"
			if checks > 1 {
				state = "SUCCEEDED"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"state": state}})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	saveDefaultToken(t)
	MetaServerURL = srv.URL
	oldInterval := appDeletePollInterval
	appDeletePollInterval = time.Millisecond
	t.Cleanup(func() { appDeletePollInterval = oldInterval })

	if err := runAppDelete("myapp", "beta", true, time.Second); err != nil {
		t.Fatalf("runAppDelete: %v", err)
	}
	if deletes != 1 || checks != 2 {
		t.Errorf("deletes = %d, checks = %d; want 1 and 2", deletes, checks)
	}
}

func TestAppDeleteKeepsUnrelatedConflict(t *testing.T) {
	const refusal = "当前应用存在 Beta 环境，请先删除 Beta 环境，再删除正式环境。"
	checks := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/make/meta/v1/app" {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 409, "msg": refusal})
			return
		}
		checks++
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"state": "NORMAL"}})
	}))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	saveDefaultToken(t)
	MetaServerURL = srv.URL

	err := runAppDelete("myapp", "prod", true, time.Second)
	if err == nil || err.Error() != refusal || checks != 1 {
		t.Fatalf("err = %v, checks = %d; want original refusal after one status check", err, checks)
	}
}

func TestAppDeleteRetriesTemporaryStatusError(t *testing.T) {
	checks := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/make/meta/v1/app" {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": true})
			return
		}
		checks++
		if checks == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 503, "msg": "暂时无法查询删除任务"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"state": "SUCCEEDED"}})
	}))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	saveDefaultToken(t)
	MetaServerURL = srv.URL
	oldInterval := appDeletePollInterval
	appDeletePollInterval = time.Millisecond
	t.Cleanup(func() { appDeletePollInterval = oldInterval })

	if err := runAppDelete("myapp", "beta", true, time.Second); err != nil {
		t.Fatalf("runAppDelete: %v", err)
	}
	if checks != 2 {
		t.Errorf("checks = %d, want 2", checks)
	}
}

func TestAppDeleteDoesNotRetryAuthFailure(t *testing.T) {
	checks := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/make/meta/v1/app" {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": true})
			return
		}
		checks++
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 990300403, "msg": "token 已失效"})
	}))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	saveDefaultToken(t)
	MetaServerURL = srv.URL

	err := runAppDelete("myapp", "beta", true, time.Second)
	if !errors.Is(err, api.ErrAuthFailed) || checks != 1 {
		t.Fatalf("err = %v, checks = %d; want auth failure after one check", err, checks)
	}
}

func TestAppDeleteConflictProbeSurfacesPermissionDenial(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/make/meta/v1/app" {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 409, "msg": "应用正在删除，请查询删除状态"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 403, "msg": "无删除应用权限"})
	}))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	saveDefaultToken(t)
	MetaServerURL = srv.URL

	err := runAppDelete("myapp", "beta", true, time.Second)
	var response *api.AppDeletionStatusResponseError
	if !errors.As(err, &response) || response.Code != 403 {
		t.Fatalf("err = %v, want permission denial from status query", err)
	}
}

func TestAppDeleteDoesNotRetryInvalidTLSCertificate(t *testing.T) {
	err := &url.Error{Op: "Get", URL: "https://meta.example", Err: x509.UnknownAuthorityError{}}
	if retryableAppDeletionStatusError(err) {
		t.Fatal("invalid TLS certificate cannot recover by retrying")
	}
}

func TestAppDeleteWaitTimeoutDoesNotReportSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/make/meta/v1/app" {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": true})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{
			"state": "RUNNING", "step": "DELETE_REPOSITORIES", "stepName": "清理代码仓库",
		}})
	}))
	defer srv.Close()
	t.Setenv("HOME", t.TempDir())
	saveDefaultToken(t)
	MetaServerURL = srv.URL
	oldInterval := appDeletePollInterval
	appDeletePollInterval = time.Millisecond
	t.Cleanup(func() { appDeletePollInterval = oldInterval })

	err := runAppDelete("myapp", "beta", true, 5*time.Millisecond)
	if !errors.Is(err, errWaitTimeout) {
		t.Fatalf("err = %v, want timeout", err)
	}
}
