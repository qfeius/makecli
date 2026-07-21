/**
 * [INPUT]: 依赖 notifier 包内 notifierEnabled / versionInChannel / shouldNotify / renderNotice（白盒）；internal/config 的通道常量
 * [OUTPUT]: 覆盖三态启用裁决、通道归属矩阵、判定链穷举、提示渲染的单元测试
 * [POS]: internal/notifier 模块 decision.go 的配套测试
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package notifier

import (
	"bytes"
	"strings"
	"testing"

	"github.com/qfeius/makecli/internal/config"
)

func boolPtr(b bool) *bool { return &b }

func TestNotifierEnabled(t *testing.T) {
	cases := []struct {
		name string
		env  string
		cfg  *bool
		want bool
	}{
		{"default on", "", nil, true},
		{"config off", "", boolPtr(false), false},
		{"config on", "", boolPtr(true), true},
		{"env off overrides config on", "false", boolPtr(true), false},
		{"env on overrides config off", "true", boolPtr(false), true},
		{"env invalid sinks to config", "garbage", boolPtr(false), false},
		{"env invalid sinks to default", "garbage", nil, true},
		{"env 0", "0", nil, false},
		{"env 1", "1", nil, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := notifierEnabled(c.env, c.cfg); got != c.want {
				t.Errorf("notifierEnabled(%q,%v) = %v, want %v", c.env, c.cfg, got, c.want)
			}
		})
	}
}

func TestVersionInChannel(t *testing.T) {
	cases := []struct {
		version string
		channel string
		want    bool
	}{
		{"1.0.0", config.ChannelStable, true},             // 正式版 ∈ stable
		{"v2.3.4", config.ChannelStable, true},            // v 前缀等价
		{"1.0.0", config.ChannelBeta, true},               // 正式版 ∈ beta（超集）
		{"v0.6.0-beta.1", config.ChannelBeta, true},       // 真 beta ∈ beta
		{"v0.6.0-beta.1", config.ChannelStable, false},    // 真 beta ∉ stable（现状语义）
		{"0.3.0-16-ga4765c1", config.ChannelBeta, false},  // git-describe 伪版本被白名单拒绝
		{"v0.6.0-rc.1", config.ChannelBeta, false},        // 非 beta.N 预发布段不进 beta 通道
		{"DEV", config.ChannelStable, false},
		{"DEV", config.ChannelBeta, false},
		{"", config.ChannelStable, false},
		{"garbage", config.ChannelBeta, false},
	}
	for _, c := range cases {
		if got := versionInChannel(c.version, c.channel); got != c.want {
			t.Errorf("versionInChannel(%q, %q) = %v, want %v", c.version, c.channel, got, c.want)
		}
	}
}

func TestShouldNotify(t *testing.T) {
	newer := cacheData{LatestVersion: "v2.0.0", Channel: config.ChannelStable}
	same := cacheData{LatestVersion: "v1.0.0", Channel: config.ChannelStable}
	empty := cacheData{Channel: config.ChannelStable}

	cases := []struct {
		name    string
		current string
		cmd     string
		tty     bool
		ci      string
		cache   cacheData
		want    bool
	}{
		{"happy path", "1.0.0", "app", true, "", newer, true},
		{"dev version", "DEV", "app", true, "", newer, false},
		{"ci set", "1.0.0", "app", true, "true", newer, false},
		{"not tty", "1.0.0", "app", false, "", newer, false},
		{"skip version cmd", "1.0.0", "version", true, "", newer, false},
		{"skip update cmd", "1.0.0", "update", true, "", newer, false},
		{"empty cmd", "1.0.0", "", true, "", newer, false},
		{"no cache", "1.0.0", "app", true, "", empty, false},
		{"same version", "1.0.0", "app", true, "", same, false},
		{"dev build vs base release (no spurious upgrade)", "0.3.0-16-ga4765c1", "app", true, "", cacheData{LatestVersion: "v0.3.0", Channel: config.ChannelStable}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shouldNotify(c.current, c.cmd, c.tty, c.ci, c.cache, config.ChannelStable); got != c.want {
				t.Errorf("shouldNotify(%q,%q,tty=%v,ci=%q) = %v, want %v",
					c.current, c.cmd, c.tty, c.ci, got, c.want)
			}
		})
	}
}

func TestShouldNotifyChannelMismatchCache(t *testing.T) {
	// 跨通道缓存不可用：beta 通道拿着 stable 缓存不提示
	cache := cacheData{LatestVersion: "v9.9.9", Channel: config.ChannelStable}
	if shouldNotify("1.0.0", "app", true, "", cache, config.ChannelBeta) {
		t.Fatal("cross-channel cache must not notify")
	}
}

func TestShouldNotifyBetaChannel(t *testing.T) {
	// beta 通道 + 真 beta current + beta 缓存 → 正常提示
	cache := cacheData{LatestVersion: "v0.6.0-beta.2", Channel: config.ChannelBeta}
	if !shouldNotify("0.6.0-beta.1", "app", true, "", cache, config.ChannelBeta) {
		t.Fatal("expected notify for newer beta on beta channel")
	}
}

func TestRenderNotice(t *testing.T) {
	var buf bytes.Buffer
	renderNotice(&buf, "1.0.0", cacheData{LatestVersion: "v2.0.0", HTMLURL: "https://example.com/r"})
	out := buf.String()
	for _, want := range []string{"1.0.0 → 2.0.0", "makecli update", "https://example.com/r"} {
		if !strings.Contains(out, want) {
			t.Errorf("notice missing %q; got:\n%s", want, out)
		}
	}
}

// 空 HTMLURL 时不应渲染 URL 行
func TestRenderNotice_NoURL(t *testing.T) {
	var buf bytes.Buffer
	renderNotice(&buf, "1.0.0", cacheData{LatestVersion: "v2.0.0"})
	out := buf.String()
	if !strings.Contains(out, "1.0.0 → 2.0.0") {
		t.Errorf("notice missing version line; got:\n%s", out)
	}
	if strings.Contains(out, "http") {
		t.Errorf("expected no URL line when HTMLURL empty; got:\n%s", out)
	}
}
