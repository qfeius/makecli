/**
 * [INPUT]: 依赖 notifier 包内 Start / Finish / isStderrTTY（白盒）；internal/build、internal/update 的测试钩子
 * [OUTPUT]: 覆盖后台刷新落盘、新鲜缓存跳过、Finish 收尾不阻塞的单元测试
 * [POS]: internal/notifier 模块 notifier.go 的配套测试，用 httptest 隔离网络
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package notifier

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/qfeius/makecli/internal/build"
	"github.com/qfeius/makecli/internal/update"
)

func setBuildVersion(t *testing.T, v string) {
	t.Helper()
	old := build.Version
	build.Version = v
	t.Cleanup(func() { build.Version = old })
}

func setTTY(t *testing.T, v bool) {
	t.Helper()
	old := isStderrTTY
	isStderrTTY = func() bool { return v }
	t.Cleanup(func() { isStderrTTY = old })
}

// mockLatest 启动 httptest 返回指定 latest release 并替换 update 的 API URL
func mockLatest(t *testing.T, tag string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"` + tag + `","html_url":"https://example.com/` + tag + `"}`))
	}))
	old := update.SetAPIBaseURLForTest(srv.URL)
	t.Cleanup(func() {
		update.SetAPIBaseURLForTest(old)
		srv.Close()
	})
}

func TestStartRefreshesCache(t *testing.T) {
	t.Setenv("MAKE_CLI_CONFIG_DIR", t.TempDir())
	setBuildVersion(t, "1.0.0")
	mockLatest(t, "v2.0.0")

	n := Start()
	<-n.done // 等后台刷新完成（测试内确定性）

	c, err := readCache()
	if err != nil {
		t.Fatalf("readCache: %v", err)
	}
	if c.LatestVersion != "v2.0.0" {
		t.Errorf("cache latest = %q, want v2.0.0", c.LatestVersion)
	}
}

func TestStartSkipsWhenFresh(t *testing.T) {
	t.Setenv("MAKE_CLI_CONFIG_DIR", t.TempDir())
	setBuildVersion(t, "1.0.0")

	if err := writeCache(cacheData{CheckedAt: time.Now(), LatestVersion: "v1.5.0"}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	mockLatest(t, "v9.9.9") // 若被请求会污染缓存

	n := Start()
	<-n.done

	c, _ := readCache()
	if c.LatestVersion != "v1.5.0" {
		t.Errorf("fresh cache should be untouched, got %q", c.LatestVersion)
	}
}

func TestFinishDisabledDoesNotBlock(t *testing.T) {
	t.Setenv("MAKE_CLI_CONFIG_DIR", t.TempDir())
	t.Setenv("MAKE_CLI_UPDATE_NOTIFIER", "false")
	setBuildVersion(t, "1.0.0")
	setTTY(t, true)
	_ = writeCache(cacheData{CheckedAt: time.Now(), LatestVersion: "v2.0.0"})

	n := &Notifier{done: make(chan struct{})}
	close(n.done)

	done := make(chan struct{})
	go func() { n.Finish("app"); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Finish blocked too long")
	}
}
