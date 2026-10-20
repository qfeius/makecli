/**
 * [INPUT]: 依赖 skillsync 的 npx / HTTP / lockfile 测试接缝与 internal/config 通道常量
 * [OUTPUT]: 验证通道切换始终执行、角色范围不变、安装来源与远端清单使用同一分支
 * [POS]: internal/skillsync 的通道集成测试，覆盖 main → beta → main 的真实编排边界
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package skillsync

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/qfeius/makecli/internal/config"
)

func TestSyncSwitchesChannelsWithoutVersionGate(t *testing.T) {
	stubNpxPresent(t)
	var commands [][]string
	setRunSkillsCommand(t, func(_ context.Context, command []string) (string, error) {
		commands = append(commands, slices.Clone(command))
		return "installed", nil
	})
	for _, tc := range []struct{ channel, branch, role string }{
		{"", "main", ""},
		{config.ChannelBeta, "beta", ""},
		{config.ChannelStable, "main", ""},
		{config.ChannelBeta, "beta", config.RoleUser},
	} {
		result, err := Sync(context.Background(), Options{Version: "v1.0.0", Channel: tc.channel, Role: tc.role})
		if err != nil {
			t.Fatal(err)
		}
		wantSource := "https://github.com/qfeius/make-platform-skills/tree/" + tc.branch
		want := []string{"npx", "-y", "skills", "add", wantSource, "--all", "-y", "--global"}
		if tc.role == config.RoleUser {
			want = []string{"npx", "-y", "skills", "add", wantSource, "-s", "makecli", "-a", "*", "-y", "--global"}
		}
		if result.Source != wantSource || !slices.Equal(commands[len(commands)-1], want) {
			t.Fatalf("channel=%q role=%q: source=%s command=%v, want %v", tc.channel, tc.role, result.Source, result.Command, want)
		}
	}
	if len(commands) != 4 {
		t.Fatalf("every switch must reinstall, got %d commands", len(commands))
	}
}

func TestPlanInstallUsesSelectedBranch(t *testing.T) {
	stubNpxPresent(t)
	stubRemoteAPI(t, func(w http.ResponseWriter, r *http.Request) {
		branch := r.URL.Query().Get("ref")
		if branch != "main" && branch != "beta" {
			t.Errorf("unexpected ref %q", branch)
		}
		_, _ = fmt.Fprintf(w, `[{"name":%q,"sha":"hash","type":"dir"}]`, branch+"-skill")
	})
	for _, tc := range []struct{ channel, branch string }{
		{config.ChannelStable, "main"}, {config.ChannelBeta, "beta"},
	} {
		for _, all := range []bool{false, true} {
			names := []string{tc.branch + "-skill"}
			plan, err := PlanInstall(context.Background(), tc.channel, names, all)
			if err != nil {
				t.Fatal(err)
			}
			wantSource := "https://github.com/qfeius/make-platform-skills/tree/" + tc.branch
			if !slices.Equal(plan.Names, names) || plan.Source != wantSource || plan.Command[4] != wantSource {
				t.Fatalf("channel=%s all=%v: mismatched catalog/source: %+v", tc.channel, all, plan)
			}
		}
	}
}

func TestListComparesSelectedBranch(t *testing.T) {
	stubLockFile(t, sampleLock)
	stubSkillsDir(t)
	stubRemoteAPI(t, func(w http.ResponseWriter, r *http.Request) {
		hash := "hash-stable"
		if r.URL.Query().Get("ref") == "beta" {
			hash = "hash-dsl"
		}
		_, _ = fmt.Fprintf(w, `[{"name":"makedsl","sha":%q,"type":"dir"}]`, hash)
	})
	for _, tc := range []struct{ channel, status string }{
		{config.ChannelStable, StatusOutdated}, {config.ChannelBeta, StatusUpToDate},
		{config.ChannelStable, StatusOutdated},
	} {
		inv := List(context.Background(), tc.channel)
		if inv.RemoteErr != nil || inv.Skills[0].Name != "makedsl" || inv.Skills[0].Status != tc.status {
			t.Fatalf("channel=%s: %+v", tc.channel, inv)
		}
	}
}
