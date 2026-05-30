/**
 * [INPUT]: 依赖 notifier 包内 notifierEnabled / shouldNotify / renderNotice（白盒）
 * [OUTPUT]: 覆盖三态启用裁决、判定链穷举、提示渲染的单元测试
 * [POS]: internal/notifier 模块 decision.go 的配套测试
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package notifier

import (
	"bytes"
	"strings"
	"testing"
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

func TestShouldNotify(t *testing.T) {
	newer := cacheData{LatestVersion: "v2.0.0"}
	same := cacheData{LatestVersion: "v1.0.0"}
	empty := cacheData{}

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
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := shouldNotify(c.current, c.cmd, c.tty, c.ci, c.cache); got != c.want {
				t.Errorf("shouldNotify(%q,%q,tty=%v,ci=%q) = %v, want %v",
					c.current, c.cmd, c.tty, c.ci, got, c.want)
			}
		})
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
