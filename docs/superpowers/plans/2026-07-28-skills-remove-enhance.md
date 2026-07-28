# makecli skills remove 增强实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `skills remove` 补齐 `--all`（从 lockfile 展开、绝不透传上游 `--all`）与确认护栏（`-y` 跳过、非 TTY 拒绝），底层逐个执行 `npx skills remove <name> -y` 并逐项汇报。

**Architecture:** 对称 install 的两阶段——`skillsync.PlanRemove`（EnsureNpx 门禁 + lockfile 校验按名/展开 `--all`）产出 `RemovePlan` 供 cmd 层确认，`skillsync.Remove` 逐个执行（每 skill 一次 npx 调用、独立超时、失败不中断）返回 `[]RemoveResult`。cmd 层与 `skills_install.go` 同构。

**Tech Stack:** Go 1.25.8、spf13/cobra、charm.land/huh/v2、mattn/go-isatty。

**Spec:** `docs/superpowers/specs/2026-07-28-skills-remove-enhance-design.md`

## Global Constraints

- Go 工具链命令（build/test/vet/lint）在命令沙箱下因 module cache 不可写会假性失败——一律禁用沙箱执行。
- 验证门控提交：`make vet && make test` **确认 exit 0** 之后才 `git commit`；禁止同一批工具调用里 test + commit。
- push 前 `golangci-lint run ./...` 必须 0 issues。
- 用 `make build / make test / make vet`，不用裸 `go` 命令。
- Edit 返回 "String to replace not found" = 编辑未生效，必须重读文件重做。
- 注释中文；GEB 头部（`[INPUT]/[OUTPUT]/[POS]/[PROTOCOL]`）；被改文件同步更新头部；任务末更新对应 CLAUDE.md。
- 无格式参数的错误用 `errors.New`。
- **安全边界（绝对）**：`--all` 从 lockfile（`source == SkillsSource`）展开为按名清单，任何情况下不把 `--all` 透传给上游 npx（上游 `--all` 会连第三方 skills 一起删除）。
- 既有测试基建事实（直接引用不再求证）：`stubLockFile(t, content string)` 与 `sampleLock`（含 Make 来源 makedsl/makeui + 第三方 swiftui-pro）在 inventory_test.go；`stubNpxPresent/stubNpxMissing` 在 env_test.go；`stubRunSkillsCommand(t, output, err) *[][]string` 当前定义在 remove_test.go——重写该文件时**必须原样保留此 helper**（sync_test.go 与 install_test.go 依赖它）。

---

### Task 1: `internal/skillsync/remove.go` — 两阶段重写 + 逐个执行

**Files:**
- Rewrite: `internal/skillsync/remove.go`（整文件替换）
- Rewrite: `internal/skillsync/remove_test.go`（整文件替换，保留 `stubRunSkillsCommand`）
- Modify: `internal/skillsync/install.go`（仅改 `Install` 的 doc 注释一行）
- Modify: `internal/skillsync/CLAUDE.md`

**Interfaces:**
- Consumes: `EnsureNpx()`、`readLock() (map[string]lockEntry, string)`、`runSkillsCommand` / `syncTimeout` / `trimOutput`、测试侧 `stubLockFile` / `sampleLock` / `stubNpxPresent` / `stubNpxMissing`。
- Produces（Task 2 依赖的确切签名）:
  - `type RemovePlan struct { Names []string; All bool; Warning string }`
  - `type RemoveResult struct { Name string; Err error }`
  - `func PlanRemove(names []string, all bool) (RemovePlan, error)`（纯本地，无 ctx）
  - `func Remove(ctx context.Context, plan RemovePlan) ([]RemoveResult, error)`
  - `func RemoveCommand(name string) []string`（单数签名）
- ⚠️ 旧 `Remove(ctx, names)` 与 `RemoveCommand(names []string)` 被替换。cmd 层唯一消费点 `cmd/skills_remove.go` 由 Task 2 重写；本任务提交后 **cmd 包暂时编译失败是预期状态**（`removeSkillsFunc = skillsync.Remove` 签名不匹配），因此本任务门禁只跑 `go vet ./internal/skillsync/ && go test ./internal/skillsync/`，全仓门禁在 Task 2 恢复。

- [ ] **Step 1: 重写失败测试 `remove_test.go`**

整文件替换为（`stubRunSkillsCommand` 与现文件逐字一致，不可改动）：

```go
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
	runSkillsCommand = func(_ context.Context, command []string) (string, error) {
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
```

- [ ] **Step 2: 跑测试确认失败**

Run（禁用沙箱）: `go test ./internal/skillsync/ -run 'TestPlanRemove|TestRemove' -v`
Expected: FAIL，`undefined: RemovePlan` 等编译错误。

- [ ] **Step 3: 重写 `remove.go`**

整文件替换为：

```go
/**
 * [INPUT]: 依赖 context、errors、fmt、maps、slices、strings
 * [OUTPUT]: 对外提供 PlanRemove / Remove / RemovePlan / RemoveResult / RemoveCommand，移除已安装的 Make platform skills
 * [POS]: internal/skillsync 的删除层，被 cmd/skills_remove.go 消费；两阶段：PlanRemove（EnsureNpx 门禁 + lockfile 校验/展开）供 cmd 层确认展示，Remove 逐个执行（每 skill 一次 npx 调用、独立超时、失败不中断）；--all 从 lockfile 展开为按名删除，绝不透传上游 --all（会误删第三方 skills）；复用 sync.go 的 runSkillsCommand / syncTimeout / trimOutput
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package skillsync

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// RemovePlan 是一次移除的已解析计划：cmd 层拿它渲染确认提示，确认后原样交给 Remove 执行。
type RemovePlan struct {
	Names   []string // 校验/展开后的待删清单（lockfile 为准，--all 时已排序）
	All     bool
	Warning string // lockfile 损坏等降级警告，cmd 层渲染 stderr
}

// RemoveResult 是单个 skill 的移除结果。
type RemoveResult struct {
	Name string
	Err  error // nil = 已移除；失败含 manual fix 与截断输出
}

// RemoveCommand 返回删除单个 skill 的非交互命令。
func RemoveCommand(name string) []string {
	return []string{"npx", "-y", "skills", "remove", name, "-y"}
}

// PlanRemove 解析一次移除：npx 环境门禁 → lockfile 校验按名 / 展开 --all。
// 名字必须都是 lockfile 中 source == SkillsSource 的已装 skill——
// 用户机器上可能有几十个第三方 skills，makecli 不越界删除；
// --all 同理从 lockfile 展开为按名清单，绝不透传上游 --all。
func PlanRemove(names []string, all bool) (RemovePlan, error) {
	if err := EnsureNpx(); err != nil {
		return RemovePlan{}, err
	}

	installed, warning := readLock()
	plan := RemovePlan{All: all, Warning: warning}

	if all {
		if len(installed) == 0 {
			return RemovePlan{}, errors.New("no Make platform skills installed")
		}
		plan.Names = slices.Sorted(maps.Keys(installed))
		return plan, nil
	}

	var invalid []string
	for _, name := range names {
		if _, ok := installed[name]; !ok {
			invalid = append(invalid, name)
		}
	}
	if len(invalid) > 0 {
		hint := "(none installed)"
		if candidates := slices.Sorted(maps.Keys(installed)); len(candidates) > 0 {
			hint = strings.Join(candidates, ", ")
		}
		if warning != "" {
			hint += fmt.Sprintf(" (warning: %s)", warning)
		}
		return RemovePlan{}, fmt.Errorf("not installed Make platform skills: %s\ninstalled Make platform skills: %s",
			strings.Join(invalid, ", "), hint)
	}
	plan.Names = names
	return plan, nil
}

// Remove 逐个执行已确认的移除计划：每个 skill 一次 npx 调用、独立超时，
// 单个失败不中断后续，逐项结果交 cmd 层渲染；计划必须来自 PlanRemove（EnsureNpx 门禁在 Plan 层）。
func Remove(ctx context.Context, plan RemovePlan) ([]RemoveResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	results := make([]RemoveResult, 0, len(plan.Names))
	failed := 0
	for _, name := range plan.Names {
		err := removeOne(ctx, name)
		results = append(results, RemoveResult{Name: name, Err: err})
		if err != nil {
			failed++
		}
	}
	if failed > 0 {
		return results, fmt.Errorf("failed to remove %d of %d skills", failed, len(plan.Names))
	}
	return results, nil
}

// removeOne 删除单个 skill，带独立超时。
func removeOne(ctx context.Context, name string) error {
	runCtx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()

	command := RemoveCommand(name)
	output, err := runSkillsCommand(runCtx, command)
	if err != nil {
		return fmt.Errorf("failed to remove %s: %w\nmanual fix: %s\n%s",
			name, err, strings.Join(command, " "), trimOutput(strings.TrimSpace(output)))
	}
	return nil
}
```

- [ ] **Step 4: install.go 契约注释收账（终审递延 Minor）**

`install.go` 中把注释行：

```go
// Install 执行已确认的安装计划。
```

改为：

```go
// Install 执行已确认的安装计划；计划必须来自 PlanInstall（EnsureNpx 门禁在 Plan 层）。
```

- [ ] **Step 5: 跑包内测试确认通过**

Run（禁用沙箱）: `go vet ./internal/skillsync/ && go test ./internal/skillsync/`
Expected: exit 0。（cmd 包此刻编译失败是预期——Task 2 恢复；**不要**跑 `make test` 全仓门禁。）

- [ ] **Step 6: 更新 `internal/skillsync/CLAUDE.md`**

修订两行：

- `remove.go`: 删除层——两阶段：PlanRemove（EnsureNpx 门禁 → readLock 校验按名 / 展开 --all，--all 从 lockfile 展开为按名清单绝不透传上游 --all，空清单报错，lockfile 警告进 RemovePlan.Warning）；Remove 逐个执行（RemoveCommand 单 skill 命令、每次独立 syncTimeout、失败不中断、汇总 failed N of M）返回 []RemoveResult；复用 readLock / runSkillsCommand / syncTimeout / trimOutput
- `remove_test.go`: 覆盖 RemoveCommand 单数构造、PlanRemove（按名校验/第三方拒绝/未安装拒绝/空 lockfile/--all 展开排序/--all 空报错/损坏 lockfile 警告/缺 npx 门禁）、Remove（逐个调用/部分失败继续并汇总）；stubNpxPresent + stubLockFile + stubRunSkillsCommand 隔离

- [ ] **Step 7: 提交**

```bash
git add internal/skillsync/remove.go internal/skillsync/remove_test.go internal/skillsync/install.go internal/skillsync/CLAUDE.md
git commit -m "feat(skillsync): remove 两阶段重写——PlanRemove 展开/校验 + 逐个执行"
```

---

### Task 2: `cmd/skills_remove.go` — --all/确认护栏改造

**Files:**
- Rewrite: `cmd/skills_remove.go`（整文件替换）
- Rewrite: `cmd/skills_remove_test.go`（整文件替换）
- Modify: `cmd/skills_install_test.go`（`TestSkillsInstallByName` 补 plan 透传断言，终审递延 Minor 收账）
- Modify: `cmd/CLAUDE.md`、根 `CLAUDE.md`

**Interfaces:**
- Consumes: `skillsync.PlanRemove(names, all) (skillsync.RemovePlan, error)`、`skillsync.Remove(ctx, plan) ([]skillsync.RemoveResult, error)`、`skillsync.RemovePlan{Names, All, Warning}`、`skillsync.RemoveResult{Name, Err}`、`skillsync.SkillsSource`（Task 1）。
- Produces: `newSkillsRemoveCmd() *cobra.Command`（签名不变，skills.go 挂载点零改动）；包级打桩变量 `planRemoveFunc` / `removeSkillsFunc` / `confirmRemoveFunc`。

- [ ] **Step 1: 重写失败测试 `cmd/skills_remove_test.go`**

整文件替换为：

```go
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
```

- [ ] **Step 2: 跑测试确认失败**

Run（禁用沙箱）: `go test ./cmd/ -run 'TestSkillsRemove|TestConfirmRemove' -v`
Expected: FAIL 编译错误（`undefined: planRemoveFunc` 等；同时旧 skills_remove.go 对新 skillsync.Remove 签名不匹配）。

- [ ] **Step 3: 重写 `cmd/skills_remove.go`**

整文件替换为：

```go
/**
 * [INPUT]: 依赖 context、errors、fmt、os、strings、charm.land/huh/v2、github.com/mattn/go-isatty、github.com/spf13/cobra、internal/skillsync
 * [OUTPUT]: 对外提供 newSkillsRemoveCmd 函数；包级 planRemoveFunc / removeSkillsFunc / confirmRemoveFunc 可打桩变量
 * [POS]: cmd/skills 的 remove 子命令：按名移除 / --all 全量互斥，缺省 huh confirm 确认（--yes 跳过，非 TTY 拒绝并指引），两阶段调用 skillsync.PlanRemove → Remove，逐项渲染结果
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"charm.land/huh/v2"
	"github.com/mattn/go-isatty"
	"github.com/qfeius/makecli/internal/skillsync"
	"github.com/spf13/cobra"
)

// planRemoveFunc / removeSkillsFunc / confirmRemoveFunc 为包级可打桩变量，
// 单测替换以隔离 lockfile、npx 执行与终端交互（参照 skills_install.go 模式）。
var planRemoveFunc = skillsync.PlanRemove
var removeSkillsFunc = skillsync.Remove
var confirmRemoveFunc = confirmRemove

func newSkillsRemoveCmd() *cobra.Command {
	var all, yes bool

	cmd := &cobra.Command{
		Use:   "remove [name]...",
		Short: "Remove installed Make platform skills",
		Example: `  makecli skills remove makedsl makeui    # 按名移除
  makecli skills remove --all             # 移除全部已装 Make platform skills
  makecli skills remove --all -y          # 跳过确认（CI / 非交互）`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillsRemove(cmd.Context(), cmd, args, all, yes)
		},
	}

	cmd.Flags().BoolVarP(&all, "all", "a", false, "remove all installed Make platform skills")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	return cmd
}

func runSkillsRemove(ctx context.Context, cmd *cobra.Command, names []string, all, yes bool) error {
	if all && len(names) > 0 {
		return errors.New("cannot use --all with skill names")
	}
	if !all && len(names) == 0 {
		return errors.New("specify skill names or --all (run 'makecli skills list' to see what's installed)")
	}

	plan, err := planRemoveFunc(names, all)
	if err != nil {
		return err
	}
	if plan.Warning != "" {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", plan.Warning)
	}

	if !yes {
		if err := confirmRemoveFunc(plan); err != nil {
			return err
		}
	}

	results, err := removeSkillsFunc(ctx, plan)
	var removed []string
	for _, r := range results {
		if r.Err == nil {
			removed = append(removed, r.Name)
		} else {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "failed %s: %v\n", r.Name, r.Err)
		}
	}
	if len(removed) > 0 {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed: %s\n", strings.Join(removed, ", "))
	}
	return err
}

// confirmRemove 在执行前确认移除计划（skills install 同款 huh confirm 护栏）。
// 非交互终端（管道 / CI）无法确认，直接拒绝并指引 --yes，杜绝挂起。
func confirmRemove(plan skillsync.RemovePlan) error {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return errors.New("refusing to remove without confirmation: re-run with --yes in a non-interactive shell")
	}

	confirmed := false
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Remove Make platform skills?").
				Description(fmt.Sprintf("Source: %s\nSkills: %s\nTarget: all detected code agents",
					skillsync.SkillsSource, strings.Join(plan.Names, ", "))).
				Affirmative("Remove").
				Negative("Abort").
				Value(&confirmed),
		),
	).Run()

	if errors.Is(err, huh.ErrUserAborted) || (err == nil && !confirmed) {
		return errors.New("remove cancelled")
	}
	return err
}
```

- [ ] **Step 4: skills_install_test.go 收账（终审递延 Minor：plan 透传断言）**

`TestSkillsInstallByName` 中，在 `if rec.installCalls != 1 {` 断言之后追加：

```go
	if !slices.Equal(rec.installPlan.Names, plan.Names) {
		t.Fatalf("plan must pass through unmodified: %+v", rec.installPlan)
	}
```

- [ ] **Step 5: 跑测试确认通过**

Run（禁用沙箱）: `go test ./cmd/ -run 'TestSkillsRemove|TestConfirmRemove|TestSkillsInstall' -v`
Expected: 全部 PASS。

- [ ] **Step 6: 全量门禁**

Run（禁用沙箱）: `make vet && make test && golangci-lint run ./...`
Expected: 全部 exit 0、0 issues。

- [ ] **Step 7: 更新文档**

`cmd/CLAUDE.md` 修订两行：

- `skills_remove.go`: skills remove 子命令——按名移除 / --all 全量互斥（都缺报错指引 skills list），两阶段 skillsync.PlanRemove（npx 门禁 + lockfile 校验/展开，--all 绝不透传上游）→ huh confirm 确认（--yes 跳过；非 TTY 拒绝指引 --yes）→ skillsync.Remove 逐个执行；逐项结果渲染（成功汇总 stdout、失败逐行 stderr）；planRemoveFunc / removeSkillsFunc / confirmRemoveFunc 包级可打桩变量
- `skills_remove_test.go`: 覆盖 skills remove 的互斥/无参报错/确认流（先 confirm 后执行、拒绝短路、-y 跳过）/警告出 stderr/逐项失败渲染/plan 与 remove 错误透传/非 TTY 真门控，stubSkillsRemove 三点打桩隔离
- 另修订 `skills_install_test.go` 行：末尾补「/plan 原样透传断言」

根 `CLAUDE.md` `internal/skillsync/` 行：把 `Remove 来源校验后透传 npx skills remove` 改为 `Remove 两阶段逐个删除（PlanRemove lockfile 校验/--all 展开，绝不透传上游 --all）`。

- [ ] **Step 8: 提交**

```bash
git add cmd/skills_remove.go cmd/skills_remove_test.go cmd/skills_install_test.go cmd/CLAUDE.md CLAUDE.md
git commit -m "feat(skills): remove --all + 确认护栏——lockfile 展开逐个删除"
```

---

### Task 3: 收尾门禁与冒烟

**Files:** 无新文件；只跑验证。

**Interfaces:**
- Consumes: Task 1–2 全部产物。
- Produces: 可合并状态（不 push）。

- [ ] **Step 1: 全量验证**

Run（禁用沙箱）: `make vet && make test && golangci-lint run ./...`
Expected: exit 0、0 issues。

- [ ] **Step 2: 构建冒烟**

Run（禁用沙箱）: `make build && bin/makecli skills remove --help`
Expected: help 正常渲染，flags 含 `-a, --all` 与 `-y, --yes`。

- [ ] **Step 3: 非交互门控冒烟**

Run: `echo | bin/makecli skills remove makedsl 2>&1 | head -5`
Expected: 报错含 `re-run with --yes`（makedsl 已装时过校验进门控）或 `not installed Make platform skills`（未装时 plan 报错）——两者都算通过，不得挂起、不得真删。

- [ ] **Step 4: 确认工作区干净**

Run: `git status --short`
Expected: 无未提交变更（`agent/`、`skills-lock.json` 既有未跟踪项除外）。
