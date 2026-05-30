/**
 * [INPUT]: 依赖 net/http、net/http/httptest、os、path/filepath、testing、time；复用 notifier_test.go 的 setBuildVersion/mockLatest 与 update.SetAPIBaseURLForTest
 * [OUTPUT]: 验证刷新失败退避落盘 + 孤儿临时文件清理的测试套件
 * [POS]: internal/notifier 的健壮性测试，配套 notifier.go 的 Start 退避与 cache.go 的 cleanStaleTemps
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package notifier

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qfeius/makecli/internal/update"
)

// mockFailingLatest 启动一个恒返回 500 的假 GitHub API，使 CheckLatest 失败
func mockFailingLatest(t *testing.T) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	old := update.SetAPIBaseURLForTest(srv.URL)
	t.Cleanup(func() {
		update.SetAPIBaseURLForTest(old)
		srv.Close()
	})
}

// TestStartBacksOffOnRefreshFailure 验证刷新失败时仍落盘退避标记（CheckedAt 前进、版本留空），
// 避免下次命令重复 spawn goroutine 并再付一次 finishDeadline 等待。
func TestStartBacksOffOnRefreshFailure(t *testing.T) {
	t.Setenv("MAKE_CLI_CONFIG_DIR", t.TempDir())
	setBuildVersion(t, "1.0.0")
	mockFailingLatest(t)

	n := Start()
	<-n.done

	cache, err := readCache()
	if err != nil {
		t.Fatalf("readCache: %v", err)
	}
	if cache.CheckedAt.IsZero() {
		t.Error("CheckedAt should advance even when refresh fails (backoff)")
	}
	if cache.LatestVersion != "" {
		t.Errorf("LatestVersion = %q, want empty on failure", cache.LatestVersion)
	}
	if cache.expired(checkInterval, time.Now()) {
		t.Error("backoff cache should be fresh, not expired")
	}
}

// TestCleanStaleTemps 验证孤儿临时文件清理：删旧、留新、不碰真实缓存文件
func TestCleanStaleTemps(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MAKE_CLI_CONFIG_DIR", dir)
	now := time.Now()
	past := now.Add(-2 * time.Hour)

	oldTmp := filepath.Join(dir, ".update-check-old.json")
	if err := os.WriteFile(oldTmp, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(oldTmp, past, past); err != nil {
		t.Fatal(err)
	}

	newTmp := filepath.Join(dir, ".update-check-new.json")
	if err := os.WriteFile(newTmp, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	} // mod time = now，年轻于阈值，应保留（可能是并发写入中）

	realCache := filepath.Join(dir, cacheFileName)
	if err := os.WriteFile(realCache, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(realCache, past, past); err != nil {
		t.Fatal(err)
	}

	cleanStaleTemps(now)

	if _, err := os.Stat(oldTmp); !os.IsNotExist(err) {
		t.Error("stale temp should be removed")
	}
	if _, err := os.Stat(newTmp); err != nil {
		t.Error("recent temp should be kept (may be a concurrent in-flight write)")
	}
	if _, err := os.Stat(realCache); err != nil {
		t.Error("real cache file must never be touched")
	}
}

// TestStartCleansStaleTempsWhenExpired 验证 Start 在过期刷新时清扫孤儿临时文件
func TestStartCleansStaleTempsWhenExpired(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MAKE_CLI_CONFIG_DIR", dir)
	setBuildVersion(t, "1.0.0")
	mockLatest(t, "v9.9.9")

	stale := filepath.Join(dir, ".update-check-stale.json")
	if err := os.WriteFile(stale, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, past, past); err != nil {
		t.Fatal(err)
	}

	n := Start()
	<-n.done

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("Start should clean stale temp on expired refresh, stat err = %v", err)
	}
}
