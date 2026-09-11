/**
 * [INPUT]: 依赖 notifier 包内 notifierEnabled / versionInChannel / pendingUpdate / newUpdate / renderNotice（白盒）；internal/config 的通道常量
 * [OUTPUT]: 覆盖三态启用裁决、通道归属矩阵、判定链穷举（含 wire 字段）、提示渲染的单元测试
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
		{"1.0.0", config.ChannelStable, true},            // 正式版 ∈ stable
		{"v2.3.4", config.ChannelStable, true},           // v 前缀等价
		{"1.0.0", config.ChannelBeta, true},              // 正式版 ∈ beta（超集）
		{"v0.6.0-beta.1", config.ChannelBeta, true},      // 真 beta ∈ beta
		{"v0.6.0-beta.1", config.ChannelStable, false},   // 真 beta ∉ stable（现状语义）
		{"0.3.0-16-ga4765c1", config.ChannelBeta, false}, // git-describe 伪版本被白名单拒绝
		{"v0.6.0-rc.1", config.ChannelBeta, false},       // 非 beta.N 预发布段不进 beta 通道
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

func TestPendingUpdate(t *testing.T) {
	newer := cacheData{LatestVersion: "v2.0.0", Channel: config.ChannelStable}
	same := cacheData{LatestVersion: "v1.0.0", Channel: config.ChannelStable}
	empty := cacheData{Channel: config.ChannelStable}

	cases := []struct {
		name    string
		current string
		cmd     string
		ci      string
		cache   cacheData
		want    bool
	}{
		{"happy path", "1.0.0", "app", "", newer, true},
		{"dev version", "DEV", "app", "", newer, false},
		{"ci set", "1.0.0", "app", "true", newer, false},
		{"skip version cmd", "1.0.0", "version", "", newer, false},
		{"skip update cmd", "1.0.0", "update", "", newer, false},
		{"empty cmd", "1.0.0", "", "", newer, false},
		{"no cache", "1.0.0", "app", "", empty, false},
		{"same version", "1.0.0", "app", "", same, false},
		{"dev build vs base release (no spurious upgrade)", "0.3.0-16-ga4765c1", "app", "", cacheData{LatestVersion: "v0.3.0", Channel: config.ChannelStable}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := pendingUpdate(c.current, c.cmd, c.ci, c.cache, config.ChannelStable)
			if (got != nil) != c.want {
				t.Errorf("pendingUpdate(%q,%q,ci=%q) = %v, want pending=%v",
					c.current, c.cmd, c.ci, got, c.want)
			}
		})
	}
}

// 判定通过时 Update 携带完整 wire 字段：版本去 v 前缀，command/message 可被 agent 直接消费
func TestPendingUpdateFields(t *testing.T) {
	cache := cacheData{LatestVersion: "v2.0.0", HTMLURL: "https://example.com/r", Channel: config.ChannelStable}
	got := pendingUpdate("v1.0.0", "app", "", cache, config.ChannelStable)
	want := Update{
		Current: "1.0.0",
		Latest:  "2.0.0",
		URL:     "https://example.com/r",
		Command: "makecli update",
		Message: "makecli 2.0.0 available, current 1.0.0, run: makecli update",
	}
	if got == nil || *got != want {
		t.Fatalf("pendingUpdate = %+v, want %+v", got, want)
	}
}

func TestPendingUpdateChannelMismatchCache(t *testing.T) {
	// 跨通道缓存不可用：beta 通道拿着 stable 缓存不提示
	cache := cacheData{LatestVersion: "v9.9.9", Channel: config.ChannelStable}
	if pendingUpdate("1.0.0", "app", "", cache, config.ChannelBeta) != nil {
		t.Fatal("cross-channel cache must not notify")
	}
}

func TestPendingUpdateBetaChannel(t *testing.T) {
	// beta 通道 + 真 beta current + beta 缓存 → 正常提示
	cache := cacheData{LatestVersion: "v0.6.0-beta.2", Channel: config.ChannelBeta}
	if pendingUpdate("0.6.0-beta.1", "app", "", cache, config.ChannelBeta) == nil {
		t.Fatal("expected pending update for newer beta on beta channel")
	}
}

// 提示框只含版本行与升级命令，不渲染 URL（URL 仅随 JSON _notice.update 给 agent）
func TestRenderNotice(t *testing.T) {
	var buf bytes.Buffer
	renderNotice(&buf, newUpdate("1.0.0", "v2.0.0", "https://example.com/r"))
	out := buf.String()
	for _, want := range []string{"1.0.0 → 2.0.0", "makecli update"} {
		if !strings.Contains(out, want) {
			t.Errorf("notice missing %q; got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "http") {
		t.Errorf("URL must not be rendered in the stderr notice; got:\n%s", out)
	}
}
