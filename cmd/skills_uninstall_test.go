/**
 * [INPUT]: 依赖 bytes、context、errors、slices、strings、testing、internal/skillsync
 * [OUTPUT]: 覆盖 skills uninstall 的互斥/无参报错/确认流/-y 跳过/警告出 stderr/逐项失败渲染/错误透传/非 TTY 门控
 * [POS]: cmd/skills uninstall 子命令测试；planUninstallFunc / uninstallSkillsFunc / confirmUninstallFunc 打桩隔离 lockfile、npx 与终端交互
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

// uninstallStubs 汇集三个打桩点的调用记录。
type uninstallStubs struct {
	planNames      []string
	planAll        bool
	planCalls      int
	confirmCalls   int
	uninstallCalls int
	uninstallPlan  skillsync.RemovePlan
}

// stubSkillsUninstall 打桩 planUninstallFunc / confirmUninstallFunc / uninstallSkillsFunc。
func stubSkillsUninstall(t *testing.T, plan skillsync.RemovePlan, results []skillsync.RemoveResult, planErr, confirmErr, uninstallErr error) *uninstallStubs {
	t.Helper()
	rec := &uninstallStubs{}

	origPlan := planUninstallFunc
	planUninstallFunc = func(names []string, all bool) (skillsync.RemovePlan, error) {
		rec.planCalls++
		rec.planNames = names
		rec.planAll = all
		return plan, planErr
	}
	origConfirm := confirmUninstallFunc
	confirmUninstallFunc = func(skillsync.RemovePlan) error {
		rec.confirmCalls++
		return confirmErr
	}
	origUninstall := uninstallSkillsFunc
	uninstallSkillsFunc = func(_ context.Context, p skillsync.RemovePlan) ([]skillsync.RemoveResult, error) {
		rec.uninstallCalls++
		rec.uninstallPlan = p
		return results, uninstallErr
	}
	t.Cleanup(func() {
		planUninstallFunc = origPlan
		confirmUninstallFunc = origConfirm
		uninstallSkillsFunc = origUninstall
	})
	return rec
}

// runUninstallCmd 构造并执行 uninstall 子命令，返回 stdout/stderr 与错误。
func runUninstallCmd(args ...string) (string, string, error) {
	cmd := newSkillsUninstallCmd()
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

func TestSkillsUninstallByName(t *testing.T) {
	plan := skillsync.RemovePlan{Names: []string{"makedsl", "makeui"}}
	rec := stubSkillsUninstall(t, plan, okResults("makedsl", "makeui"), nil, nil, nil)

	out, _, err := runUninstallCmd("makedsl", "makeui", "-y")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if rec.planAll || !slices.Equal(rec.planNames, []string{"makedsl", "makeui"}) {
		t.Fatalf("unexpected plan args: names=%v all=%v", rec.planNames, rec.planAll)
	}
	if rec.uninstallCalls != 1 || !slices.Equal(rec.uninstallPlan.Names, plan.Names) {
		t.Fatalf("plan must pass through unmodified: %+v", rec.uninstallPlan)
	}
	if !strings.Contains(out, "makedsl, makeui skills uninstalled.") {
		t.Fatalf("missing success output:\n%s", out)
	}
}

func TestSkillsUninstallAll(t *testing.T) {
	plan := skillsync.RemovePlan{All: true, Names: []string{"makedsl", "makeui"}}
	rec := stubSkillsUninstall(t, plan, okResults("makedsl", "makeui"), nil, nil, nil)

	out, _, err := runUninstallCmd("--all", "-y")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !rec.planAll || len(rec.planNames) != 0 {
		t.Fatalf("unexpected plan args: names=%v all=%v", rec.planNames, rec.planAll)
	}
	if !strings.Contains(out, "makedsl, makeui skills uninstalled.") {
		t.Fatalf("missing success output:\n%s", out)
	}
}

func TestSkillsUninstallAllWithNamesRejected(t *testing.T) {
	rec := stubSkillsUninstall(t, skillsync.RemovePlan{}, nil, nil, nil, nil)

	_, _, err := runUninstallCmd("--all", "makedsl")
	if err == nil || !strings.Contains(err.Error(), "cannot use --all with skill names") {
		t.Fatalf("expected mutual-exclusion error, got: %v", err)
	}
	if rec.planCalls != 0 {
		t.Fatalf("plan must not run, got %d calls", rec.planCalls)
	}
}

func TestSkillsUninstallRequiresNamesOrAll(t *testing.T) {
	rec := stubSkillsUninstall(t, skillsync.RemovePlan{}, nil, nil, nil, nil)

	_, _, err := runUninstallCmd()
	if err == nil || !strings.Contains(err.Error(), "--all") {
		t.Fatalf("expected usage error, got: %v", err)
	}
	if rec.planCalls != 0 {
		t.Fatalf("plan must not run, got %d calls", rec.planCalls)
	}
}

func TestSkillsUninstallConfirmThenUninstall(t *testing.T) {
	plan := skillsync.RemovePlan{Names: []string{"makedsl"}}
	rec := stubSkillsUninstall(t, plan, okResults("makedsl"), nil, nil, nil)

	if _, _, err := runUninstallCmd("makedsl"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if rec.confirmCalls != 1 || rec.uninstallCalls != 1 {
		t.Fatalf("expected confirm=1 uninstall=1, got confirm=%d uninstall=%d", rec.confirmCalls, rec.uninstallCalls)
	}
}

func TestSkillsUninstallConfirmDeclineShortCircuits(t *testing.T) {
	plan := skillsync.RemovePlan{Names: []string{"makedsl"}}
	rec := stubSkillsUninstall(t, plan, nil, nil, errors.New("uninstall cancelled"), nil)

	_, _, err := runUninstallCmd("makedsl")
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("expected cancellation error, got: %v", err)
	}
	if rec.uninstallCalls != 0 {
		t.Fatalf("uninstall must not run after decline, got %d calls", rec.uninstallCalls)
	}
}

func TestSkillsUninstallYesSkipsConfirm(t *testing.T) {
	plan := skillsync.RemovePlan{Names: []string{"makedsl"}}
	rec := stubSkillsUninstall(t, plan, okResults("makedsl"), nil, errors.New("must not be called"), nil)

	if _, _, err := runUninstallCmd("makedsl", "-y"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if rec.confirmCalls != 0 {
		t.Fatalf("confirm must be skipped with -y, got %d calls", rec.confirmCalls)
	}
}

func TestSkillsUninstallWarningGoesToStderr(t *testing.T) {
	plan := skillsync.RemovePlan{Names: []string{"makedsl"}, Warning: "lock schema mismatch"}
	stubSkillsUninstall(t, plan, okResults("makedsl"), nil, nil, nil)

	out, errOut, err := runUninstallCmd("makedsl", "-y")
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

func TestSkillsUninstallPlanErrorPropagates(t *testing.T) {
	rec := stubSkillsUninstall(t, skillsync.RemovePlan{}, nil, errors.New("not installed Make platform skills: nope"), nil, nil)

	_, _, err := runUninstallCmd("nope", "-y")
	if err == nil || !strings.Contains(err.Error(), "not installed Make platform skills") {
		t.Fatalf("expected plan error, got: %v", err)
	}
	if rec.uninstallCalls != 0 {
		t.Fatalf("uninstall must not run on plan error, got %d calls", rec.uninstallCalls)
	}
}

func TestSkillsUninstallPartialFailureRendersResults(t *testing.T) {
	plan := skillsync.RemovePlan{Names: []string{"makedsl", "makeui"}}
	results := []skillsync.RemoveResult{
		{Name: "makedsl"},
		{Name: "makeui", Err: errors.New("exit 1")},
	}
	stubSkillsUninstall(t, plan, results, nil, nil, errors.New("failed to uninstall 1 of 2 skills"))

	out, errOut, err := runUninstallCmd("makedsl", "makeui", "-y")
	if err == nil || !strings.Contains(err.Error(), "failed to uninstall 1 of 2 skills") {
		t.Fatalf("expected summary error, got: %v", err)
	}
	if !strings.Contains(errOut, "failed makeui: exit 1") {
		t.Fatalf("failed item missing from stderr:\n%s", errOut)
	}
	if !strings.Contains(out, "makedsl skill uninstalled.") {
		t.Fatalf("succeeded item missing from stdout:\n%s", out)
	}
}

func TestConfirmUninstallNonInteractiveRejects(t *testing.T) {
	// go test 环境 stdin 非 TTY，真门控直接生效。
	err := confirmUninstall(skillsync.RemovePlan{Names: []string{"makedsl"}})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected non-interactive rejection hinting --yes, got: %v", err)
	}
}
