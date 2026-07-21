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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/qfeius/makecli/internal/build"
	"github.com/qfeius/makecli/internal/config"
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

	if err := writeCache(cacheData{CheckedAt: time.Now(), LatestVersion: "v1.5.0", Channel: config.ChannelStable}); err != nil {
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

func TestStartRefreshesBetaChannel(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(config.EnvConfigDir, dir)
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte("[settings]\nchannel = beta\n"), 0600); err != nil {
		t.Fatal(err)
	}
	setBuildVersion(t, "0.5.5")

	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`[{"tag_name":"v9.9.9-beta.1","prerelease":true,"html_url":"https://example.com/r"}]`))
	}))
	defer srv.Close()
	old := update.SetAPIBaseURLForTest(srv.URL)
	defer update.SetAPIBaseURLForTest(old)

	n := Start()
	<-n.done

	if gotPath != "/repos/qfeius/makecli/releases" {
		t.Fatalf("path = %s, want /repos/qfeius/makecli/releases (beta 走列表端点)", gotPath)
	}
	cache, err := readCache()
	if err != nil {
		t.Fatal(err)
	}
	if cache.Channel != config.ChannelBeta {
		t.Fatalf("cache.Channel = %q, want beta", cache.Channel)
	}
	if cache.LatestVersion != "v9.9.9-beta.1" {
		t.Fatalf("cache.LatestVersion = %q, want v9.9.9-beta.1", cache.LatestVersion)
	}
}

func TestStartRefreshesOnChannelSwitch(t *testing.T) {
	// 新鲜但跨通道的缓存必须触发刷新
	dir := t.TempDir()
	t.Setenv(config.EnvConfigDir, dir)
	if err := os.WriteFile(filepath.Join(dir, "config"), []byte("[settings]\nchannel = beta\n"), 0600); err != nil {
		t.Fatal(err)
	}
	setBuildVersion(t, "0.5.5")
	if err := writeCache(cacheData{CheckedAt: time.Now(), LatestVersion: "v0.5.5", Channel: config.ChannelStable}); err != nil {
		t.Fatal(err)
	}

	requested := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = true
		_, _ = w.Write([]byte(`[{"tag_name":"v0.6.0-beta.1","prerelease":true}]`))
	}))
	defer srv.Close()
	old := update.SetAPIBaseURLForTest(srv.URL)
	defer update.SetAPIBaseURLForTest(old)

	n := Start()
	<-n.done

	if !requested {
		t.Fatal("fresh-but-cross-channel cache must trigger refresh")
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
