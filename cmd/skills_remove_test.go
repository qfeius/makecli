/**
 * [INPUT]: 依赖 bytes、context、errors、slices、strings、testing、internal/skillsync
 * [OUTPUT]: 覆盖 skills remove 的互斥/无参报错/确认流/-y 跳过/警告出 stderr/逐项失败渲染/错误透传/非 TTY 门控
 * [POS]: cmd/skills remove 子命令测试；planRemoveFunc / removeSkillsFunc / confirmRemoveFunc 打桩隔离 lockfile、npx 与终端交互
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/qfeius/makecli/internal/skillsync"
)

// removeStubs 汇集三个打桩点的调用记录。
type removeStubs struct {
	planNames    []string
	planAll      bool
	planCalls    int
	confirmCalls int
	removeCalls  int
	removePlan   skillsync.RemovePlan
}

// stubSkillsRemove 打桩 planRemoveFunc / confirmRemoveFunc / removeSkillsFunc。
func stubSkillsRemove(t *testing.T, plan skillsync.RemovePlan, results []skillsync.RemoveResult, planErr, confirmErr, removeErr error) *removeStubs {
	t.Helper()
	rec := &removeStubs{}

	origPlan := planRemoveFunc
	planRemoveFunc = func(names []string, all bool) (skillsync.RemovePlan, error) {
		rec.planCalls++
		rec.planNames = names
		rec.planAll = all
		return plan, planErr
	}
	origConfirm := confirmRemoveFunc
	confirmRemoveFunc = func(skillsync.RemovePlan) error {
		rec.confirmCalls++
		return confirmErr
	}
	origRemove := removeSkillsFunc
	removeSkillsFunc = func(_ context.Context, p skillsync.RemovePlan) ([]skillsync.RemoveResult, error) {
		rec.removeCalls++
		rec.removePlan = p
		return results, removeErr
	}
	t.Cleanup(func() {
		planRemoveFunc = origPlan
		confirmRemoveFunc = origConfirm
		removeSkillsFunc = origRemove
	})
	return rec
}

// runRemoveCmd 构造并执行 remove 子命令，返回 stdout/stderr 与错误。
func runRemoveCmd(args ...string) (string, string, error) {
	cmd := newSkillsRemoveCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

func okResults(names ...string) []skillsync.RemoveResult {
	results := make([]skillsync.RemoveResult, 0, len(names))
	for _, name := range names {
		results = append(results, skillsync.RemoveResult{Name: name})
	}
	return results
}

func TestSkillsRemoveByName(t *testing.T) {
	plan := skillsync.RemovePlan{Names: []string{"makedsl", "makeui"}}
	rec := stubSkillsRemove(t, plan, okResults("makedsl", "makeui"), nil, nil, nil)

	out, _, err := runRemoveCmd("makedsl", "makeui", "-y")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if rec.planAll || !slices.Equal(rec.planNames, []string{"makedsl", "makeui"}) {
		t.Fatalf("unexpected plan args: names=%v all=%v", rec.planNames, rec.planAll)
	}
	if rec.removeCalls != 1 || !slices.Equal(rec.removePlan.Names, plan.Names) {
		t.Fatalf("plan must pass through unmodified: %+v", rec.removePlan)
	}
	if !strings.Contains(out, "Removed: makedsl, makeui") {
		t.Fatalf("missing success output:\n%s", out)
	}
}

func TestSkillsRemoveAll(t *testing.T) {
	plan := skillsync.RemovePlan{All: true, Names: []string{"makedsl", "makeui"}}
	rec := stubSkillsRemove(t, plan, okResults("makedsl", "makeui"), nil, nil, nil)

	out, _, err := runRemoveCmd("--all", "-y")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !rec.planAll || len(rec.planNames) != 0 {
		t.Fatalf("unexpected plan args: names=%v all=%v", rec.planNames, rec.planAll)
	}
	if !strings.Contains(out, "Removed: makedsl, makeui") {
		t.Fatalf("missing success output:\n%s", out)
	}
}

func TestSkillsRemoveAllWithNamesRejected(t *testing.T) {
	rec := stubSkillsRemove(t, skillsync.RemovePlan{}, nil, nil, nil, nil)

	_, _, err := runRemoveCmd("--all", "makedsl")
	if err == nil || !strings.Contains(err.Error(), "cannot use --all with skill names") {
		t.Fatalf("expected mutual-exclusion error, got: %v", err)
	}
	if rec.planCalls != 0 {
		t.Fatalf("plan must not run, got %d calls", rec.planCalls)
	}
}

func TestSkillsRemoveRequiresNamesOrAll(t *testing.T) {
	rec := stubSkillsRemove(t, skillsync.RemovePlan{}, nil, nil, nil, nil)

	_, _, err := runRemoveCmd()
	if err == nil || !strings.Contains(err.Error(), "--all") {
		t.Fatalf("expected usage error, got: %v", err)
	}
	if rec.planCalls != 0 {
		t.Fatalf("plan must not run, got %d calls", rec.planCalls)
	}
}

func TestSkillsRemoveConfirmThenRemove(t *testing.T) {
	plan := skillsync.RemovePlan{Names: []string{"makedsl"}}
	rec := stubSkillsRemove(t, plan, okResults("makedsl"), nil, nil, nil)

	if _, _, err := runRemoveCmd("makedsl"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if rec.confirmCalls != 1 || rec.removeCalls != 1 {
		t.Fatalf("expected confirm=1 remove=1, got confirm=%d remove=%d", rec.confirmCalls, rec.removeCalls)
	}
}

func TestSkillsRemoveConfirmDeclineShortCircuits(t *testing.T) {
	plan := skillsync.RemovePlan{Names: []string{"makedsl"}}
	rec := stubSkillsRemove(t, plan, nil, nil, errors.New("remove cancelled"), nil)

	_, _, err := runRemoveCmd("makedsl")
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("expected cancellation error, got: %v", err)
	}
	if rec.removeCalls != 0 {
		t.Fatalf("remove must not run after decline, got %d calls", rec.removeCalls)
	}
}

func TestSkillsRemoveYesSkipsConfirm(t *testing.T) {
	plan := skillsync.RemovePlan{Names: []string{"makedsl"}}
	rec := stubSkillsRemove(t, plan, okResults("makedsl"), nil, errors.New("must not be called"), nil)

	if _, _, err := runRemoveCmd("makedsl", "-y"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if rec.confirmCalls != 0 {
		t.Fatalf("confirm must be skipped with -y, got %d calls", rec.confirmCalls)
	}
}

func TestSkillsRemoveWarningGoesToStderr(t *testing.T) {
	plan := skillsync.RemovePlan{Names: []string{"makedsl"}, Warning: "lock schema mismatch"}
	stubSkillsRemove(t, plan, okResults("makedsl"), nil, nil, nil)

	out, errOut, err := runRemoveCmd("makedsl", "-y")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(errOut, "warning: lock schema mismatch") {
		t.Fatalf("warning missing from stderr:\n%s", errOut)
	}
	if strings.Contains(out, "warning:") {
		t.Fatalf("warning must not pollute stdout:\n%s", out)
	}
}

func TestSkillsRemovePlanErrorPropagates(t *testing.T) {
	rec := stubSkillsRemove(t, skillsync.RemovePlan{}, nil, errors.New("not installed Make platform skills: nope"), nil, nil)

	_, _, err := runRemoveCmd("nope", "-y")
	if err == nil || !strings.Contains(err.Error(), "not installed Make platform skills") {
		t.Fatalf("expected plan error, got: %v", err)
	}
	if rec.removeCalls != 0 {
		t.Fatalf("remove must not run on plan error, got %d calls", rec.removeCalls)
	}
}

func TestSkillsRemovePartialFailureRendersResults(t *testing.T) {
	plan := skillsync.RemovePlan{Names: []string{"makedsl", "makeui"}}
	results := []skillsync.RemoveResult{
		{Name: "makedsl"},
		{Name: "makeui", Err: errors.New("exit 1")},
	}
	stubSkillsRemove(t, plan, results, nil, nil, errors.New("failed to remove 1 of 2 skills"))

	out, errOut, err := runRemoveCmd("makedsl", "makeui", "-y")
	if err == nil || !strings.Contains(err.Error(), "failed to remove 1 of 2 skills") {
		t.Fatalf("expected summary error, got: %v", err)
	}
	if !strings.Contains(errOut, "failed makeui: exit 1") {
		t.Fatalf("failed item missing from stderr:\n%s", errOut)
	}
	if !strings.Contains(out, "Removed: makedsl") {
		t.Fatalf("succeeded item missing from stdout:\n%s", out)
	}
}

func TestConfirmRemoveNonInteractiveRejects(t *testing.T) {
	// go test 环境 stdin 非 TTY，真门控直接生效。
	err := confirmRemove(skillsync.RemovePlan{Names: []string{"makedsl"}})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected non-interactive rejection hinting --yes, got: %v", err)
	}
}
