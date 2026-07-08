/**
 * [INPUT]: 依赖 context、encoding/json、strings、testing、internal/skillsync
 * [OUTPUT]: 覆盖 runSkillsList（table/json/空态/警告/非法格式）与 skills 默认行为 = list
 * [POS]: cmd/skills list 子命令测试，stubListSkills 打桩隔离 lockfile 与网络
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/qfeius/makecli/internal/skillsync"
)

// stubListSkills 打桩 listSkillsFunc 返回固定 Inventory。
func stubListSkills(t *testing.T, inv skillsync.Inventory) {
	t.Helper()
	orig := listSkillsFunc
	listSkillsFunc = func(ctx context.Context) skillsync.Inventory { return inv }
	t.Cleanup(func() { listSkillsFunc = orig })
}

func sampleInventory() skillsync.Inventory {
	return skillsync.Inventory{Skills: []skillsync.SkillInfo{
		{Name: "make-app-auth", Status: skillsync.StatusNotInstalled, RemoteHash: "d"},
		{Name: "makedsl", Status: skillsync.StatusOutdated, Description: "DSL 设计与生成", UpdatedAt: "2026-07-02T00:00:00.000Z", LocalHash: "a", RemoteHash: "b"},
		{Name: "makeui", Status: skillsync.StatusUpToDate, Description: "页面布局", UpdatedAt: "2026-07-01T00:00:00.000Z", LocalHash: "c", RemoteHash: "c"},
	}}
}

func TestRunSkillsListTable(t *testing.T) {
	stubListSkills(t, sampleInventory())

	out := captureStdout(t, func() {
		if err := runSkillsList(context.Background(), outputTable); err != nil {
			t.Errorf("runSkillsList: %v", err)
		}
	})

	for _, want := range []string{"NAME", "STATUS", "makedsl", "outdated", "makeui", "up-to-date", "make-app-auth", "not installed"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "2 installed, 1 outdated, 1 available") {
		t.Errorf("missing summary line:\n%s", out)
	}
	if !strings.Contains(out, "makecli skills update") {
		t.Errorf("missing update hint:\n%s", out)
	}
	// UPDATED AT 裁到日期
	if !strings.Contains(out, "2026-07-02") || strings.Contains(out, "00:00:00") {
		t.Errorf("updated at must be date-only:\n%s", out)
	}
}

func TestRunSkillsListJSON(t *testing.T) {
	stubListSkills(t, sampleInventory())

	out := captureStdout(t, func() {
		if err := runSkillsList(context.Background(), outputJSON); err != nil {
			t.Errorf("runSkillsList: %v", err)
		}
	})

	var payload struct {
		Data []skillsync.SkillInfo `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if len(payload.Data) != 3 {
		t.Fatalf("expected 3 skills, got %d", len(payload.Data))
	}
	if payload.Data[1].Description != "DSL 设计与生成" {
		t.Fatalf("JSON must keep full description: %+v", payload.Data[1])
	}
}

func TestRunSkillsListEmpty(t *testing.T) {
	stubListSkills(t, skillsync.Inventory{})

	out := captureStdout(t, func() {
		if err := runSkillsList(context.Background(), outputTable); err != nil {
			t.Errorf("runSkillsList: %v", err)
		}
	})

	if !strings.Contains(out, "No Make platform skills installed") {
		t.Errorf("missing empty state:\n%s", out)
	}
	if !strings.Contains(out, "makecli skills update") {
		t.Errorf("empty state must guide installation:\n%s", out)
	}
}

func TestRunSkillsListWarnings(t *testing.T) {
	inv := sampleInventory()
	inv.LockWarning = "schema version is 2"
	inv.RemoteErr = context.DeadlineExceeded
	stubListSkills(t, inv)

	var stdout string
	stderr := captureStderr(t, func() {
		stdout = captureStdout(t, func() {
			if err := runSkillsList(context.Background(), outputTable); err != nil {
				t.Errorf("warnings must not fail the command: %v", err)
			}
		})
	})

	if !strings.Contains(stderr, "schema version is 2") {
		t.Errorf("stderr missing lock warning:\n%s", stderr)
	}
	if !strings.Contains(stderr, "remote check failed") {
		t.Errorf("stderr missing remote warning:\n%s", stderr)
	}
	if !strings.Contains(stdout, "makedsl") {
		t.Errorf("table must still render on warnings:\n%s", stdout)
	}
}

func TestRunSkillsListInvalidOutput(t *testing.T) {
	if err := runSkillsList(context.Background(), "xml"); err == nil {
		t.Fatal("expected error for invalid output format")
	}
}

func TestSkillsDefaultIsList(t *testing.T) {
	stubListSkills(t, sampleInventory())

	cmd := newSkillsCmd()
	cmd.SetArgs([]string{})
	out := captureStdout(t, func() {
		if err := cmd.Execute(); err != nil {
			t.Errorf("execute: %v", err)
		}
	})

	if !strings.Contains(out, "makedsl") {
		t.Errorf("bare 'makecli skills' must render list:\n%s", out)
	}
}

func TestTruncateLine(t *testing.T) {
	if got := truncateLine("短描述", 60); got != "短描述" {
		t.Fatalf("short string must pass through, got %q", got)
	}
	long := strings.Repeat("很", 70)
	got := truncateLine(long, 60)
	if len([]rune(got)) != 61 || !strings.HasSuffix(got, "…") {
		t.Fatalf("expected 60 runes + ellipsis, got %q", got)
	}
}
