/**
 * [INPUT]: 依赖 doctor.go 的 runDoctor / errDoctorFailed / doctorFixHint；setProfile / setAccessTokenFlag / captureStdout 测试辅助；internal/config 隔离配置与凭证；internal/skillsync 清单桩；regexp
 * [OUTPUT]: 覆盖 doctor 的单元测试（默认只读：旧键标 fixable 指引 --fix 且不改文件退出 1 / --fix 搬家到 context 且其后命令可用 / 健康配置 OK 退出 0 / 未知 context、channel 报 settings set 指引与缺 token 报问题退出 1 / --fix 修复后同轮 context 检查读到新键 / 哨兵静默 / --fix flag 注册 / 默认完整输出与失败后继续列 skills / --fix 自更新与同步、失败可见 / 移除 --detail）；squash + hasLine 让行断言不依赖名字列宽（列宽随最长检查名浮动）
 * [POS]: cmd 模块 doctor.go 的配套测试
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package cmd

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/qfeius/makecli/internal/config"
	"github.com/qfeius/makecli/internal/skillsync"
	"github.com/qfeius/makecli/internal/update"
)

// squash 把连续空格压成一个：doctor 的名字列按最长检查名对齐，断言不该依赖列宽。
func squash(s string) string { return regexp.MustCompile(` {2,}`).ReplaceAllString(s, " ") }

// hasLine 拼出 squash 后一行的期望形态："<mark> <name> <msg>"。
func hasLine(mark, name, msg string) string { return mark + " " + name + " " + msg }

// healthyDoctorEnv 准备一份健康的隔离配置：token 来自 flag（不读凭证文件），无旧键。
func healthyDoctorEnv(t *testing.T) {
	t.Helper()
	t.Setenv(config.EnvConfigDir, t.TempDir())
	t.Setenv(EnvContext, "")
	t.Setenv(EnvAccessToken, "")
	setContextFlag(t, "")
	setProfile(t, "default")
	setAccessTokenFlag(t, "tok")
}

func TestDoctorReadOnlyByDefault(t *testing.T) {
	healthyDoctorEnv(t)
	if err := config.SetSetting("environment", "dev"); err != nil {
		t.Fatal(err)
	}

	var err error
	out := squash(captureStdout(t, func() { err = runDoctor(false) }))
	if !errors.Is(err, errDoctorFailed) {
		t.Fatalf("read-only doctor must report the fixable problem as failure, got %v\n%s", err, out)
	}
	for _, want := range []string{hasLine("✗", "settings", "outdated key(s): environment (fixable, run: "+doctorFixHint+")"), "FAIL: 1 problem left"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "fixed") {
		t.Errorf("read-only doctor must not fix anything:\n%s", out)
	}
	// 文件未被触碰：旧键仍在，解析链仍拒绝
	s, _ := config.LoadSettings()
	if s.Legacy["environment"] != "dev" || s.Context != "" {
		t.Errorf("read-only doctor modified settings: %+v", s)
	}
	if _, _, err := resolveContext(); err == nil {
		t.Error("legacy config should still be refused after read-only doctor")
	}
}

func TestDoctorFixMigratesLegacyEnvironment(t *testing.T) {
	healthyDoctorEnv(t)
	if err := config.SetSetting("environment", "dev"); err != nil {
		t.Fatal(err)
	}

	var err error
	out := squash(captureStdout(t, func() { err = runDoctor(true) }))
	if err != nil {
		t.Fatalf("runDoctor --fix: %v\n%s", err, out)
	}
	for _, want := range []string{hasLine("✗", "settings", "outdated key(s): environment"), hasLine("✓", "fixed", "renamed [settings] environment → context"), hasLine("✓", "context", "dev"), "OK: configuration is healthy"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "(fixable") {
		t.Errorf("--fix mode must not print the fixable hint:\n%s", out)
	}

	// 迁移后：文件只剩新键，解析链恢复且值保留
	s, _ := config.LoadSettings()
	if s.Context != "dev" || s.Legacy != nil {
		t.Errorf("post-migration settings = %+v", s)
	}
	name, _, err := resolveContext()
	if err != nil || name != "dev" {
		t.Errorf("resolveContext after doctor --fix = %q, %v", name, err)
	}
}

func TestDoctorHealthy(t *testing.T) {
	for _, fix := range []bool{false, true} {
		healthyDoctorEnv(t)
		var err error
		out := squash(captureStdout(t, func() { err = runDoctor(fix) }))
		if err != nil {
			t.Fatalf("runDoctor(fix=%v): %v\n%s", fix, err, out)
		}
		for _, want := range []string{"✓ settings", hasLine("✓", "context", "not set, defaults to production"), hasLine("✓", "channel", "not set, defaults to stable"), hasLine("✓", "token", `profile "default" (from flag)`), "OK: configuration is healthy"} {
			if !strings.Contains(out, want) {
				t.Errorf("fix=%v: output missing %q:\n%s", fix, want, out)
			}
		}
		if strings.Contains(out, "✗") {
			t.Errorf("fix=%v: healthy config should report no problems:\n%s", fix, out)
		}
	}
}

func TestDoctorReportsUnfixableProblems(t *testing.T) {
	t.Run("unknown context and channel", func(t *testing.T) {
		healthyDoctorEnv(t)
		if err := config.SetSetting("context", "staging"); err != nil {
			t.Fatal(err)
		}
		if err := config.SetSetting("channel", "nightly"); err != nil {
			t.Fatal(err)
		}
		var err error
		out := squash(captureStdout(t, func() { err = runDoctor(true) }))
		if !errors.Is(err, errDoctorFailed) {
			t.Fatalf("expected errDoctorFailed, got %v", err)
		}
		for _, want := range []string{hasLine("✗", "context", `unknown context "staging"`), "makecli settings set context <value>", hasLine("✗", "channel", `unknown channel "nightly"`), "makecli settings set channel <value>", "FAIL: 2 problems left"} {
			if !strings.Contains(out, want) {
				t.Errorf("output missing %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, "(fixable") {
			t.Errorf("unfixable problems must not be marked fixable:\n%s", out)
		}
	})

	t.Run("missing token points to login", func(t *testing.T) {
		healthyDoctorEnv(t)
		setAccessTokenFlag(t, "")
		setProfile(t, "work")
		var err error
		out := squash(captureStdout(t, func() { err = runDoctor(false) }))
		if !errors.Is(err, errDoctorFailed) {
			t.Fatalf("expected errDoctorFailed, got %v", err)
		}
		if !strings.Contains(out, hasLine("✗", "token", `profile "work" has no access token — run: makecli login --profile work`)) {
			t.Errorf("output missing login guidance:\n%s", out)
		}
	})

	t.Run("credentials token counts", func(t *testing.T) {
		healthyDoctorEnv(t)
		setAccessTokenFlag(t, "")
		if err := config.Save(config.Credentials{"default": {AccessToken: "file-tok"}}); err != nil {
			t.Fatal(err)
		}
		var err error
		out := squash(captureStdout(t, func() { err = runDoctor(false) }))
		if err != nil {
			t.Fatalf("runDoctor: %v\n%s", err, out)
		}
		if !strings.Contains(out, hasLine("✓", "token", `profile "default" (from credentials)`)) {
			t.Errorf("output missing credentials source:\n%s", out)
		}
	})
}

func TestDoctorFixLegacyValueStillValidated(t *testing.T) {
	// 旧键搬家后值原样保留；若旧值本身非法，同一轮 context 检查须读到新键并报问题
	healthyDoctorEnv(t)
	if err := config.SetSetting("environment", "staging"); err != nil {
		t.Fatal(err)
	}
	var err error
	out := squash(captureStdout(t, func() { err = runDoctor(true) }))
	if !errors.Is(err, errDoctorFailed) {
		t.Fatalf("expected errDoctorFailed, got %v", err)
	}
	for _, want := range []string{hasLine("✓", "fixed", "renamed [settings] environment → context"), hasLine("✗", "context", `unknown context "staging"`)} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestDoctorSentinelIsSilent(t *testing.T) {
	var buf bytes.Buffer
	reportExecuteError(&buf, errDoctorFailed)
	if buf.Len() != 0 {
		t.Errorf("errDoctorFailed should be silent, got %q", buf.String())
	}
	if ExitCode(errDoctorFailed) != 1 {
		t.Errorf("ExitCode(errDoctorFailed) = %d, want 1", ExitCode(errDoctorFailed))
	}
}

func TestDoctorFixFlagRegistered(t *testing.T) {
	cmd := newDoctorCmd("DEV", "")
	f := cmd.Flags().Lookup("fix")
	if f == nil {
		t.Fatal("doctor should register --fix")
	}
	if f.DefValue != "false" {
		t.Errorf("--fix must default to false (read-only doctor), got %q", f.DefValue)
	}
}

// 默认完整报告；--fix 复用真实 update 编排，网络与副作用在边界打桩。
func TestDoctorReport(t *testing.T) {
	for _, tc := range []struct {
		name                           string
		fix, missingToken, updateFails bool
	}{
		{name: "healthy"},
		{name: "missing token", missingToken: true},
		{name: "fix upgrades", fix: true},
		{name: "fix continues after diagnostic failure", fix: true, missingToken: true},
		{name: "update failure is visible", fix: true, missingToken: true, updateFails: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			healthyDoctorEnv(t)
			if tc.missingToken {
				setAccessTokenFlag(t, "")
			}
			setBuildVersion(t, "v1.2.3")
			status := 0
			if tc.updateFails {
				status = 500
			}
			t.Cleanup(mockReleaseServer(t, status, update.Release{TagName: "v1.2.4"}))
			applied := setApplyFunc(t, noopApply)
			synced := setSyncSkillsSuccess(t)
			original := listSkillsFunc
			t.Cleanup(func() { listSkillsFunc = original })
			calls := 0
			listSkillsFunc = func(context.Context) skillsync.Inventory {
				calls++
				if tc.fix && !tc.updateFails && len(*synced) != 1 {
					t.Error("skills list must run after update sync")
				}
				return skillsync.Inventory{}
			}
			cmd := newDoctorCmd("v1.2.3", "2026-10-09")
			if cmd.Flags().Lookup("detail") != nil {
				t.Fatal("--detail must be removed")
			}
			if tc.fix {
				cmd.SetArgs([]string{"--fix"})
			} else {
				cmd.SetArgs([]string{})
			}
			cmd.SetErr(&bytes.Buffer{})
			var err error
			out := captureStdout(t, func() { err = cmd.Execute() })
			wantFailure := tc.missingToken || tc.updateFails
			if (err != nil) != wantFailure {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.missingToken && !tc.updateFails && !errors.Is(err, errDoctorFailed) {
				t.Fatalf("diagnostic failure lost: %v", err)
			}
			if tc.updateFails {
				var rendered bytes.Buffer
				reportExecuteError(&rendered, err)
				if rendered.Len() == 0 {
					t.Fatal("update failure was silenced")
				}
			}
			wantUpdate := tc.fix && !tc.updateFails
			if *applied != wantUpdate || (len(*synced) == 1) != wantUpdate {
				t.Fatalf("update apply=%v, sync=%d", *applied, len(*synced))
			}
			version := formatVersion("v1.2.3", "2026-10-09")
			diagnostic := strings.Index(out, "Config:")
			skills := strings.Index(out, "No Make platform skills installed.")
			if !strings.HasPrefix(out, version) || diagnostic < len(version) || skills < diagnostic || calls != 1 {
				t.Fatalf("unexpected report order or list calls (%d): %s", calls, out)
			}
		})
	}
}
