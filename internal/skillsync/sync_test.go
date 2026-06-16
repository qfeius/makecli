/**
 * [INPUT]: 依赖 context、testing；白盒替换 runSkillsCommand
 * [OUTPUT]: 覆盖 Make platform skills 同步器的强制同步、跳过和失败提示
 * [POS]: internal/skillsync 的单元测试，隔离 npx 副作用并验证 update 默认每次刷新 skills
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package skillsync

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func setRunSkillsCommand(t *testing.T, f func(context.Context, []string) (string, error)) {
	t.Helper()
	old := runSkillsCommand
	runSkillsCommand = f
	t.Cleanup(func() { runSkillsCommand = old })
}

func TestSyncAlwaysRunsNpx(t *testing.T) {
	var gotCommand []string
	setRunSkillsCommand(t, func(_ context.Context, command []string) (string, error) {
		gotCommand = append([]string{}, command...)
		return "installed", nil
	})

	result, err := Sync(context.Background(), Options{Version: "v0.4.0"})
	if err != nil {
		t.Fatalf("Sync returned error: %v", err)
	}

	if result.Action != ActionSynced {
		t.Fatalf("action = %q, want %q", result.Action, ActionSynced)
	}
	if !slices.Equal(gotCommand, SkillsCommand()) {
		t.Fatalf("command = %#v, want %#v", gotCommand, SkillsCommand())
	}
	if result.CommandString() != "npx -y skills add qfeius/make-platform-skills --all -y" {
		t.Fatalf("command string = %q", result.CommandString())
	}
}

func TestSyncRunsNpxEveryTime(t *testing.T) {
	calls := 0
	setRunSkillsCommand(t, func(context.Context, []string) (string, error) {
		calls++
		return "installed", nil
	})

	for i := 0; i < 2; i++ {
		result, err := Sync(context.Background(), Options{Version: "v0.4.0"})
		if err != nil {
			t.Fatalf("Sync returned error: %v", err)
		}
		if result.Action != ActionSynced {
			t.Fatalf("action = %q, want %q", result.Action, ActionSynced)
		}
	}
	if calls != 2 {
		t.Fatalf("runSkillsCommand calls = %d, want 2", calls)
	}
}

func TestSyncSkipOptionDoesNotRun(t *testing.T) {
	setRunSkillsCommand(t, func(context.Context, []string) (string, error) {
		t.Fatal("runSkillsCommand should not be called when Skip is true")
		return "", nil
	})

	result, err := Sync(context.Background(), Options{Version: "v0.4.0", Skip: true})
	if err != nil {
		t.Fatalf("Sync returned error: %v", err)
	}
	if result.Action != ActionSkipped || result.Reason != "disabled by flag" {
		t.Fatalf("result = %+v", result)
	}
}

func TestSyncCommandFailureIncludesManualCommandAndOutput(t *testing.T) {
	setRunSkillsCommand(t, func(context.Context, []string) (string, error) {
		return "registry timeout", errors.New("exit status 1")
	})

	result, err := Sync(context.Background(), Options{Version: "v0.4.0"})
	if err == nil {
		t.Fatal("expected sync error")
	}
	if result.Action != ActionFailed {
		t.Fatalf("action = %q, want %q", result.Action, ActionFailed)
	}
	if !strings.Contains(err.Error(), "npx -y skills add qfeius/make-platform-skills --all -y") {
		t.Fatalf("error missing manual command: %v", err)
	}
	if !strings.Contains(err.Error(), "registry timeout") {
		t.Fatalf("error missing command output: %v", err)
	}
}
