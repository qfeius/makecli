/**
 * [INPUT]: 依赖 context、errors、net/http、slices、strings、testing
 * [OUTPUT]: 覆盖 InstallCommand 构造、PlanInstall 校验/降级/展开/npx 门禁、Install 执行与失败路径
 * [POS]: internal/skillsync 安装层测试；stubNpxPresent + stubRemoteAPI + stubRunSkillsCommand 隔离 PATH、网络与 npx
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package skillsync

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"
	"testing"
)

// serveSampleRemote 返回固定远端清单（inventory_test.go 的 sampleRemote）。
func serveSampleRemote(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte(sampleRemote))
}

func TestInstallCommandByName(t *testing.T) {
	got := InstallCommand([]string{"makedsl", "makeui"}, false)
	want := []string{"npx", "-y", "skills", "add", SkillsSource, "-s", "makedsl", "makeui", "-a", "*", "-y"}
	if !slices.Equal(got, want) {
		t.Fatalf("unexpected command:\n got %v\nwant %v", got, want)
	}
}

func TestInstallCommandAllReusesSkillsCommand(t *testing.T) {
	if got := InstallCommand(nil, true); !slices.Equal(got, SkillsCommand()) {
		t.Fatalf("--all must reuse SkillsCommand, got %v", got)
	}
}

func TestPlanInstallValidNames(t *testing.T) {
	stubNpxPresent(t)
	stubRemoteAPI(t, serveSampleRemote)

	plan, err := PlanInstall(context.Background(), []string{"makedsl"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slices.Equal(plan.Names, []string{"makedsl"}) {
		t.Fatalf("unexpected names: %v", plan.Names)
	}
	if plan.Warning != "" {
		t.Fatalf("unexpected warning: %q", plan.Warning)
	}
	if !slices.Equal(plan.Command, InstallCommand([]string{"makedsl"}, false)) {
		t.Fatalf("unexpected command: %v", plan.Command)
	}
}

func TestPlanInstallDedupesAndSortsNames(t *testing.T) {
	stubNpxPresent(t)
	stubRemoteAPI(t, serveSampleRemote)

	plan, err := PlanInstall(context.Background(), []string{"makeui", "makedsl", "makeui"}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slices.Equal(plan.Names, []string{"makedsl", "makeui"}) {
		t.Fatalf("expected deduped sorted names, got %v", plan.Names)
	}
	if !slices.Equal(plan.Command, InstallCommand([]string{"makedsl", "makeui"}, false)) {
		t.Fatalf("command must be built from deduped names, got %v", plan.Command)
	}
}

func TestPlanInstallUnknownNameListsCandidates(t *testing.T) {
	stubNpxPresent(t)
	stubRemoteAPI(t, serveSampleRemote)

	_, err := PlanInstall(context.Background(), []string{"makedsl", "nope"}, false)
	if err == nil {
		t.Fatal("expected error for unknown skill name")
	}
	for _, want := range []string{"unknown Make platform skills: nope", "available skills:", "makedsl"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error missing %q:\n%s", want, err)
		}
	}
}

func TestPlanInstallRemoteUnreachableDegrades(t *testing.T) {
	stubNpxPresent(t)
	stubRemoteAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	plan, err := PlanInstall(context.Background(), []string{"makedsl"}, false)
	if err != nil {
		t.Fatalf("remote failure must degrade, not fail: %v", err)
	}
	if !slices.Equal(plan.Names, []string{"makedsl"}) {
		t.Fatalf("names must pass through: %v", plan.Names)
	}
	if !strings.Contains(plan.Warning, "cannot verify skill names") {
		t.Fatalf("expected verify warning, got %q", plan.Warning)
	}
}

func TestPlanInstallAllExpandsRemote(t *testing.T) {
	stubNpxPresent(t)
	stubRemoteAPI(t, serveSampleRemote)

	plan, err := PlanInstall(context.Background(), nil, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !slices.Equal(plan.Names, []string{"make-app-auth", "makedsl", "makeui"}) {
		t.Fatalf("expected sorted remote dirs, got %v", plan.Names)
	}
	if !slices.Equal(plan.Command, SkillsCommand()) {
		t.Fatalf("unexpected command: %v", plan.Command)
	}
}

func TestPlanInstallAllRemoteUnreachable(t *testing.T) {
	stubNpxPresent(t)
	stubRemoteAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	plan, err := PlanInstall(context.Background(), nil, true)
	if err != nil {
		t.Fatalf("remote failure must degrade, not fail: %v", err)
	}
	if len(plan.Names) != 0 {
		t.Fatalf("names must be empty when remote unknown: %v", plan.Names)
	}
	if !strings.Contains(plan.Warning, "cannot list remote skills") {
		t.Fatalf("expected list warning, got %q", plan.Warning)
	}
}

func TestPlanInstallWithoutNpx(t *testing.T) {
	stubNpxMissing(t)
	remoteCalls := 0
	stubRemoteAPI(t, func(w http.ResponseWriter, _ *http.Request) {
		remoteCalls++
		_, _ = w.Write([]byte(sampleRemote))
	})

	_, err := PlanInstall(context.Background(), []string{"makedsl"}, false)
	if err == nil || !strings.Contains(err.Error(), "npx not found") {
		t.Fatalf("expected npx guidance error, got: %v", err)
	}
	if remoteCalls != 0 {
		t.Fatalf("must fail before touching remote, got %d calls", remoteCalls)
	}
}

func TestInstallExecutesPlanCommand(t *testing.T) {
	calls := stubRunSkillsCommand(t, "installed", nil)
	plan := InstallPlan{Command: InstallCommand([]string{"makedsl"}, false)}

	if err := Install(context.Background(), plan); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*calls) != 1 || !slices.Equal((*calls)[0], plan.Command) {
		t.Fatalf("unexpected calls: %v", *calls)
	}
}

func TestInstallFailureIncludesManualFix(t *testing.T) {
	stubRunSkillsCommand(t, "boom output", errors.New("exit 1"))
	plan := InstallPlan{Command: InstallCommand([]string{"makedsl"}, false)}

	err := Install(context.Background(), plan)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"failed to install skills", "manual fix: npx -y skills add", "boom output"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error missing %q:\n%s", want, err)
		}
	}
}
