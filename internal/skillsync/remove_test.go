/**
 * [INPUT]: 依赖 context、errors、slices、strings、testing
 * [OUTPUT]: 覆盖 RemoveCommand 构造与 Remove 的来源校验/执行/失败路径
 * [POS]: internal/skillsync 删除层测试，白盒替换 runSkillsCommand 避免真实执行 npx
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

// stubRunSkillsCommand 替换 runSkillsCommand，记录调用并返回给定结果。
func stubRunSkillsCommand(t *testing.T, output string, err error) *[][]string {
	t.Helper()
	var calls [][]string
	orig := runSkillsCommand
	runSkillsCommand = func(ctx context.Context, command []string) (string, error) {
		calls = append(calls, command)
		return output, err
	}
	t.Cleanup(func() { runSkillsCommand = orig })
	return &calls
}

func TestRemoveCommand(t *testing.T) {
	got := RemoveCommand([]string{"makedsl", "makeui"})
	want := []string{"npx", "-y", "skills", "remove", "makedsl", "makeui", "-y"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRemoveExecutesCommand(t *testing.T) {
	stubLockFile(t, sampleLock)
	calls := stubRunSkillsCommand(t, "removed", nil)

	if err := Remove(context.Background(), []string{"makedsl"}); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if len(*calls) != 1 {
		t.Fatalf("expected 1 command execution, got %d", len(*calls))
	}
	if !slices.Equal((*calls)[0], RemoveCommand([]string{"makedsl"})) {
		t.Fatalf("unexpected command: %v", (*calls)[0])
	}
}

func TestRemoveRejectsThirdPartySkill(t *testing.T) {
	stubLockFile(t, sampleLock) // swiftui-pro 是第三方来源
	calls := stubRunSkillsCommand(t, "", nil)

	err := Remove(context.Background(), []string{"swiftui-pro"})

	if err == nil {
		t.Fatal("expected error for third-party skill")
	}
	if !strings.Contains(err.Error(), "swiftui-pro") {
		t.Fatalf("error must name the invalid skill: %v", err)
	}
	if !strings.Contains(err.Error(), "makedsl") || !strings.Contains(err.Error(), "makeui") {
		t.Fatalf("error must list installed candidates: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatal("must not execute command when validation fails")
	}
}

func TestRemoveNotInstalledName(t *testing.T) {
	stubLockFile(t, sampleLock)
	calls := stubRunSkillsCommand(t, "", nil)

	err := Remove(context.Background(), []string{"no-such-skill"})

	if err == nil || !strings.Contains(err.Error(), "no-such-skill") {
		t.Fatalf("expected error naming unknown skill, got %v", err)
	}
	if len(*calls) != 0 {
		t.Fatal("must not execute command when validation fails")
	}
}

func TestRemoveEmptyLockfile(t *testing.T) {
	stubLockFile(t, "")
	stubRunSkillsCommand(t, "", nil)

	err := Remove(context.Background(), []string{"makedsl"})

	if err == nil || !strings.Contains(err.Error(), "none installed") {
		t.Fatalf("expected '(none installed)' hint, got %v", err)
	}
}

func TestRemoveCommandFailure(t *testing.T) {
	stubLockFile(t, sampleLock)
	stubRunSkillsCommand(t, "boom output", errors.New("exit 1"))

	err := Remove(context.Background(), []string{"makedsl"})

	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "manual fix") || !strings.Contains(err.Error(), "boom output") {
		t.Fatalf("error must carry manual fix and output: %v", err)
	}
}
