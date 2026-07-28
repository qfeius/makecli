/**
 * [INPUT]: 依赖 context、errors、slices、strings、testing
 * [OUTPUT]: 覆盖 PlanRemove 校验/展开/门禁与 Remove 逐个执行/部分失败汇总；stubRunSkillsCommand 供 sync/install 测试复用
 * [POS]: internal/skillsync 删除层测试；stubNpxPresent + stubLockFile + stubRunSkillsCommand 隔离 PATH、文件系统与 npx
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

func TestRemoveCommandSingle(t *testing.T) {
	got := RemoveCommand("makedsl")
	want := []string{"npx", "-y", "skills", "remove", "makedsl", "-y"}
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected command:\n got %v\nwant %v", got, want)
	}
}

func TestPlanRemoveValidNames(t *testing.T) {
	stubNpxPresent(t)
	stubLockFile(t, sampleLock)

	plan, err := PlanRemove([]string{"makedsl"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slices.Equal(plan.Names, []string{"makedsl"}) {
		t.Fatalf("unexpected names: %v", plan.Names)
	}
	if plan.All || plan.Warning != "" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
}

func TestPlanRemoveRejectsThirdPartySkill(t *testing.T) {
	stubNpxPresent(t)
	stubLockFile(t, sampleLock) // swiftui-pro 是第三方来源

	_, err := PlanRemove([]string{"swiftui-pro"}, false)
	if err == nil {
		t.Fatal("expected error for third-party skill")
	}
	for _, want := range []string{"not installed Make platform skills: swiftui-pro", "makedsl, makeui"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error missing %q:\n%s", want, err)
		}
	}
}

func TestPlanRemoveNotInstalledName(t *testing.T) {
	stubNpxPresent(t)
	stubLockFile(t, sampleLock)

	_, err := PlanRemove([]string{"makedsl", "nope"}, false)
	if err == nil || !strings.Contains(err.Error(), "not installed Make platform skills: nope") {
		t.Fatalf("expected not-installed error, got: %v", err)
	}
}

func TestPlanRemoveEmptyLockfile(t *testing.T) {
	stubNpxPresent(t)
	stubLockFile(t, "")

	_, err := PlanRemove([]string{"makedsl"}, false)
	if err == nil || !strings.Contains(err.Error(), "(none installed)") {
		t.Fatalf("expected none-installed hint, got: %v", err)
	}
}

func TestPlanRemoveAllExpandsLockfile(t *testing.T) {
	stubNpxPresent(t)
	stubLockFile(t, sampleLock)

	plan, err := PlanRemove(nil, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slices.Equal(plan.Names, []string{"makedsl", "makeui"}) {
		t.Fatalf("expected sorted make skills (third-party excluded), got %v", plan.Names)
	}
	if !plan.All {
		t.Fatal("plan.All must be true")
	}
}

func TestPlanRemoveAllEmptyLockfile(t *testing.T) {
	stubNpxPresent(t)
	stubLockFile(t, "")

	_, err := PlanRemove(nil, true)
	if err == nil || !strings.Contains(err.Error(), "no Make platform skills installed") {
		t.Fatalf("expected empty error, got: %v", err)
	}
}

func TestPlanRemoveCorruptLockfileSurfacesWarning(t *testing.T) {
	stubNpxPresent(t)
	stubLockFile(t, "{not json")

	_, err := PlanRemove([]string{"makedsl"}, false)
	if err == nil || !strings.Contains(err.Error(), "warning:") {
		t.Fatalf("expected warning in error, got: %v", err)
	}
}

func TestPlanRemoveWithoutNpx(t *testing.T) {
	stubNpxMissing(t)
	stubLockFile(t, sampleLock)

	_, err := PlanRemove([]string{"makedsl"}, false)
	if err == nil || !strings.Contains(err.Error(), "npx not found") {
		t.Fatalf("expected npx guidance error, got: %v", err)
	}
}

func TestRemoveExecutesPerName(t *testing.T) {
	calls := stubRunSkillsCommand(t, "removed", nil)

	results, err := Remove(context.Background(), RemovePlan{Names: []string{"makedsl", "makeui"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 || results[0].Err != nil || results[1].Err != nil {
		t.Fatalf("unexpected results: %+v", results)
	}
	if len(*calls) != 2 {
		t.Fatalf("expected one npx call per skill, got %d", len(*calls))
	}
	if !slices.Equal((*calls)[0], RemoveCommand("makedsl")) || !slices.Equal((*calls)[1], RemoveCommand("makeui")) {
		t.Fatalf("unexpected commands: %v", *calls)
	}
}

func TestRemovePartialFailureContinues(t *testing.T) {
	// 第一个失败、后续继续执行：按调用次数分支返回。
	var n int
	orig := runSkillsCommand
	runSkillsCommand = func(_ context.Context, _ []string) (string, error) {
		n++
		if n == 1 {
			return "boom output", errors.New("exit 1")
		}
		return "removed", nil
	}
	t.Cleanup(func() { runSkillsCommand = orig })

	results, err := Remove(context.Background(), RemovePlan{Names: []string{"makedsl", "makeui"}})
	if err == nil || !strings.Contains(err.Error(), "failed to remove 1 of 2 skills") {
		t.Fatalf("expected summary error, got: %v", err)
	}
	if results[0].Err == nil || !strings.Contains(results[0].Err.Error(), "manual fix: npx -y skills remove makedsl -y") {
		t.Fatalf("first result must fail with manual fix: %+v", results[0])
	}
	if results[1].Err != nil {
		t.Fatalf("second result must succeed: %+v", results[1])
	}
	if n != 2 {
		t.Fatalf("failure must not stop the loop, got %d calls", n)
	}
}
