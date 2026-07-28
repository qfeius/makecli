# makecli skills install 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 为 makecli 增加 `skills install` 子命令：按名选装 / `--all` 全量，安装前 huh confirm 确认（`-y` 跳过），npx 环境前置检查（`EnsureNpx`）盖住 Sync/Remove/Install 三动词。

**Architecture:** 两阶段安装——`skillsync.PlanInstall`（npx 门禁 + 远端清单校验 + 构造命令）产出 `InstallPlan` 供 cmd 层渲染确认，确认后 `skillsync.Install` 经既有 `runSkillsCommand` seam 执行 npx。多 agent 分发由上游 vercel-labs/skills CLI 负责，makecli 只编排。

**Tech Stack:** Go 1.25.8、spf13/cobra、charm.land/huh/v2（确认表单）、mattn/go-isatty（TTY 检测）、httptest（GitHub API 隔离）。

**Spec:** `docs/superpowers/specs/2026-07-28-skills-install-design.md`

## Global Constraints

- Go 工具链命令（build/test/vet/lint）在命令沙箱下因 module cache 不可写会假性失败——一律禁用沙箱执行。
- 验证门控提交：`make vet && make test` **确认 exit 0** 之后才 `git commit`；禁止同一批工具调用里 test + commit。
- push 前 `golangci-lint run ./...` 必须 0 issues（gocritic 常见坑：手写线性查找改 `slices.Contains`、`[]byte` 比较改 `bytes.Equal`）。
- 用 `make build / make test / make vet`，不用裸 `go` 命令。
- Edit 返回 "String to replace not found" = 编辑未生效，必须重读文件重做。
- 注释中文；每个新文件带 GEB 头部（`[INPUT]/[OUTPUT]/[POS]/[PROTOCOL]`）；被改文件同步更新自身头部；任务末更新对应目录 CLAUDE.md。
- 无格式参数的错误用 `errors.New`，不用 `fmt.Errorf`（lint 会拦）。
- 上游命令事实（已验证，直接引用不再求证）：`npx skills add` 的 `-s` 单 flag 贪婪收集空格分隔多值；`--all` = `--skill '*' --agent '*' -y`；命令经 `exec.CommandContext` 直传，`*` 无 glob 展开。

---

### Task 1: `internal/skillsync/env.go` — EnsureNpx 环境门禁

**Files:**
- Create: `internal/skillsync/env.go`
- Create: `internal/skillsync/env_test.go`
- Modify: `internal/skillsync/sync.go`（Sync 前置 EnsureNpx，位于 Skip 判断之后）
- Modify: `internal/skillsync/remove.go`（Remove 顶部前置 EnsureNpx）
- Modify: `internal/skillsync/sync_test.go`、`internal/skillsync/remove_test.go`（既有测试补 `stubNpxPresent`）
- Modify: `internal/skillsync/CLAUDE.md`

**Interfaces:**
- Consumes: 无（本任务是底座）。
- Produces: `func EnsureNpx() error`（npx 缺失时返回带 How-to-fix 指引的错误）；测试 helper `stubNpxPresent(t *testing.T)` / `stubNpxMissing(t *testing.T)`（包内共享，Task 2 复用）；包级接缝 `var lookPathFunc = exec.LookPath`。

- [ ] **Step 1: 写失败测试 `env_test.go`**

```go
/**
 * [INPUT]: 依赖 errors、strings、testing
 * [OUTPUT]: 覆盖 EnsureNpx 的存在/缺失路径；提供 stubNpxPresent / stubNpxMissing 供 sync/remove/install 测试复用
 * [POS]: internal/skillsync 环境门禁层测试，lookPathFunc 打桩隔离 PATH
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package skillsync

import (
	"errors"
	"strings"
	"testing"
)

// stubNpxPresent 打桩 lookPathFunc 模拟 npx 存在，被 sync/remove/install 测试复用保持隔离。
func stubNpxPresent(t *testing.T) {
	t.Helper()
	orig := lookPathFunc
	lookPathFunc = func(file string) (string, error) { return "/usr/local/bin/" + file, nil }
	t.Cleanup(func() { lookPathFunc = orig })
}

// stubNpxMissing 打桩 lookPathFunc 模拟 npx 缺失。
func stubNpxMissing(t *testing.T) {
	t.Helper()
	orig := lookPathFunc
	lookPathFunc = func(string) (string, error) { return "", errors.New("executable file not found in $PATH") }
	t.Cleanup(func() { lookPathFunc = orig })
}

func TestEnsureNpxPresent(t *testing.T) {
	stubNpxPresent(t)
	if err := EnsureNpx(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureNpxMissingGivesGuidance(t *testing.T) {
	stubNpxMissing(t)
	err := EnsureNpx()
	if err == nil {
		t.Fatal("expected error when npx missing")
	}
	for _, want := range []string{"npx not found", "How to fix", "brew install node", "https://nodejs.org"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error missing %q:\n%s", want, err)
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run（禁用沙箱）: `go test ./internal/skillsync/ -run TestEnsureNpx -v`
Expected: FAIL，`undefined: lookPathFunc` / `undefined: EnsureNpx` 编译错误。

- [ ] **Step 3: 实现 `env.go`**

```go
/**
 * [INPUT]: 依赖 errors、os/exec
 * [OUTPUT]: 对外提供 EnsureNpx；lookPathFunc 为测试接缝
 * [POS]: internal/skillsync 的环境门禁层，Sync / Remove / PlanInstall 在 shell out npx 前统一调用
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package skillsync

import (
	"errors"
	"os/exec"
)

// lookPathFunc 是 PATH 查找接缝，测试注入以模拟 npx 缺失。
var lookPathFunc = exec.LookPath

// EnsureNpx 确认 npx 可用。Make platform skills 经 'skills' npm CLI 分发，
// 缺 npx 时 exec 的报错晦涩、失败信息里的 manual fix 命令也没法跑，
// 这里换成面向 agent 一步收敛的安装指引。
func EnsureNpx() error {
	if _, err := lookPathFunc("npx"); err != nil {
		return errors.New(`npx not found: Make platform skills are distributed via the 'skills' npm CLI
How to fix:
  macOS:  brew install node
  or install Node.js (npx ships with npm): https://nodejs.org
Then re-run the command`)
	}
	return nil
}
```

- [ ] **Step 4: 跑测试确认通过**

Run（禁用沙箱）: `go test ./internal/skillsync/ -run TestEnsureNpx -v`
Expected: PASS ×2。

- [ ] **Step 5: 写 Sync/Remove 接入的失败测试**

在 `sync_test.go` 追加（`stubRunSkillsCommand` 定义在 `remove_test.go`，同包直接用）：

```go
func TestSyncFailsWithoutNpx(t *testing.T) {
	stubNpxMissing(t)
	calls := stubRunSkillsCommand(t, "", nil)

	result, err := Sync(context.Background(), Options{Version: "v1.0.0"})
	if err == nil || !strings.Contains(err.Error(), "npx not found") {
		t.Fatalf("expected npx guidance error, got: %v", err)
	}
	if result.Action != ActionFailed {
		t.Fatalf("expected action %q, got %q", ActionFailed, result.Action)
	}
	if len(*calls) != 0 {
		t.Fatalf("npx must not be executed when missing, got %d calls", len(*calls))
	}
}

func TestSyncSkipDoesNotRequireNpx(t *testing.T) {
	stubNpxMissing(t)

	result, err := Sync(context.Background(), Options{Version: "v1.0.0", Skip: true})
	if err != nil {
		t.Fatalf("skip must not require npx: %v", err)
	}
	if result.Action != ActionSkipped {
		t.Fatalf("expected action %q, got %q", ActionSkipped, result.Action)
	}
}
```

在 `remove_test.go` 追加：

```go
func TestRemoveFailsWithoutNpx(t *testing.T) {
	stubNpxMissing(t)
	calls := stubRunSkillsCommand(t, "", nil)

	err := Remove(context.Background(), []string{"makedsl"})
	if err == nil || !strings.Contains(err.Error(), "npx not found") {
		t.Fatalf("expected npx guidance error, got: %v", err)
	}
	if len(*calls) != 0 {
		t.Fatalf("npx must not be executed when missing, got %d calls", len(*calls))
	}
}
```

- [ ] **Step 6: 跑新测试确认失败**

Run（禁用沙箱）: `go test ./internal/skillsync/ -run 'TestSyncFailsWithoutNpx|TestSyncSkipDoesNotRequireNpx|TestRemoveFailsWithoutNpx' -v`
Expected: FAIL——Sync/Remove 尚未接入 EnsureNpx，`expected npx guidance error, got: <nil>`。

- [ ] **Step 7: 接入 Sync 与 Remove**

`sync.go`：在 `if opts.Skip { ... return result, nil }` 块之后、`runCtx := ctx` 之前插入：

```go
	if err := EnsureNpx(); err != nil {
		result.Action = ActionFailed
		return result, err
	}
```

`remove.go`：`Remove` 函数体第一行（`installed, warning := readLock()` 之前）插入：

```go
	if err := EnsureNpx(); err != nil {
		return err
	}
```

同步更新两个文件的 GEB 头部（`[POS]` 注明前置 EnsureNpx 门禁）。

- [ ] **Step 8: 既有测试补 npx 桩**

以下每个测试函数体第一行加 `stubNpxPresent(t)`（保持机器无 npx 也能跑）：

- `sync_test.go`：`TestSyncAlwaysRunsNpx`、`TestSyncRunsNpxEveryTime`、`TestSyncCommandFailureIncludesManualCommandAndOutput`（`TestSyncSkipOptionDoesNotRun` 不需要——Skip 在门禁前返回）。
- `remove_test.go`：`TestRemoveExecutesCommand`、`TestRemoveRejectsThirdPartySkill`、`TestRemoveNotInstalledName`、`TestRemoveEmptyLockfile`、`TestRemoveCorruptLockfileSurfacesWarning`、`TestRemoveCommandFailure`（`TestRemoveCommand` 只测命令构造，不需要）。

- [ ] **Step 9: 全包测试 + vet 确认通过**

Run（禁用沙箱）: `make vet && make test`
Expected: 全部 exit 0。

- [ ] **Step 10: 更新 `internal/skillsync/CLAUDE.md`**

成员清单追加两行、修订两行：

- `env.go`: 环境门禁层——EnsureNpx 确认 npx 可用，缺失时输出 How-to-fix 安装指引（brew / nodejs.org）；lookPathFunc 为测试接缝；Sync / Remove / PlanInstall 在 shell out 前统一调用
- `env_test.go`: 覆盖 EnsureNpx 存在/缺失路径；stubNpxPresent / stubNpxMissing 供 sync/remove/install 测试复用
- `sync.go` 行补注：Skip 判断后前置 EnsureNpx（Skip 不要求 npx）
- `remove.go` 行补注：入口前置 EnsureNpx

- [ ] **Step 11: 提交**

```bash
git add internal/skillsync/env.go internal/skillsync/env_test.go internal/skillsync/sync.go internal/skillsync/remove.go internal/skillsync/sync_test.go internal/skillsync/remove_test.go internal/skillsync/CLAUDE.md
git commit -m "feat(skillsync): EnsureNpx 环境门禁，Sync/Remove 前置检查"
```

---

### Task 2: `internal/skillsync/install.go` — 两阶段安装层

**Files:**
- Create: `internal/skillsync/install.go`
- Create: `internal/skillsync/install_test.go`
- Modify: `internal/skillsync/CLAUDE.md`

**Interfaces:**
- Consumes: `EnsureNpx()`、`stubNpxPresent/stubNpxMissing`（Task 1）；既有 `runSkillsCommand` / `syncTimeout` / `trimOutput` / `SkillsCommand()` / `SkillsSource`（sync.go）、`fetchRemoteSkills(ctx) (map[string]string, error)` / `stubRemoteAPI` / `sampleRemote`（inventory.go / inventory_test.go）、`stubRunSkillsCommand`（remove_test.go）。
- Produces（Task 3 依赖的确切签名）:
  - `type InstallPlan struct { Names []string; All bool; Command []string; Warning string }`
  - `func PlanInstall(ctx context.Context, names []string, all bool) (InstallPlan, error)`
  - `func Install(ctx context.Context, plan InstallPlan) error`
  - `func InstallCommand(names []string, all bool) []string`

- [ ] **Step 1: 写失败测试 `install_test.go`**

`sampleRemote`（inventory_test.go）含目录 `makedsl`、`makeui`、`make-app-auth` 与一个 file 类型条目；排序后目录名为 `[make-app-auth, makedsl, makeui]`。

```go
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
```

- [ ] **Step 2: 跑测试确认失败**

Run（禁用沙箱）: `go test ./internal/skillsync/ -run 'TestInstall|TestPlanInstall' -v`
Expected: FAIL，`undefined: InstallCommand` 等编译错误。

- [ ] **Step 3: 实现 `install.go`**

```go
/**
 * [INPUT]: 依赖 context、fmt、maps、slices、strings
 * [OUTPUT]: 对外提供 PlanInstall / Install / InstallPlan / InstallCommand，安装指定或全部 Make platform skills
 * [POS]: internal/skillsync 的安装层，被 cmd/skills_install.go 消费；两阶段：PlanInstall（EnsureNpx 门禁 + 远端校验 + 构造命令）供 cmd 层确认展示，Install 执行；复用 sync.go 的 runSkillsCommand / syncTimeout / trimOutput 与 inventory.go 的 fetchRemoteSkills
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package skillsync

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// InstallPlan 是一次安装的已解析计划：cmd 层拿它渲染确认提示，确认后原样交给 Install 执行。
type InstallPlan struct {
	Names   []string // 将安装的 skill 名；--all 且远端不可达时为空（装什么由 npx 裁决）
	All     bool
	Command []string // 将执行的 npx 命令
	Warning string   // 远端不可达降级时的提示，cmd 层渲染到 stderr
}

// InstallCommand 返回安装命令：--all 与 update 同一条命令（同一语义）；
// 按名走上游 -s（单 flag 贪婪收集空格分隔多值）+ -a '*'（全部 agent，与 --all 行为一致）。
func InstallCommand(names []string, all bool) []string {
	if all {
		return SkillsCommand()
	}
	command := []string{"npx", "-y", "skills", "add", SkillsSource, "-s"}
	command = append(command, names...)
	return append(command, "-a", "*", "-y")
}

// PlanInstall 解析一次安装：npx 环境门禁 → 远端清单校验/展开 → 构造命令。
// 按名拼错即报错并列出可用名字；远端不可达是降级不是门禁（Warning 提示，npx 裁决）。
func PlanInstall(ctx context.Context, names []string, all bool) (InstallPlan, error) {
	if err := EnsureNpx(); err != nil {
		return InstallPlan{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}

	plan := InstallPlan{All: all}
	remote, err := fetchRemoteSkills(ctx)
	switch {
	case all && err == nil:
		plan.Names = slices.Sorted(maps.Keys(remote))
	case all:
		plan.Warning = fmt.Sprintf("cannot list remote skills: %v", err)
	case err != nil:
		plan.Names = names
		plan.Warning = fmt.Sprintf("cannot verify skill names against %s: %v", SkillsSource, err)
	default:
		var invalid []string
		for _, name := range names {
			if _, ok := remote[name]; !ok {
				invalid = append(invalid, name)
			}
		}
		if len(invalid) > 0 {
			return InstallPlan{}, fmt.Errorf("unknown Make platform skills: %s\navailable skills: %s",
				strings.Join(invalid, ", "), strings.Join(slices.Sorted(maps.Keys(remote)), ", "))
		}
		plan.Names = names
	}
	plan.Command = InstallCommand(names, all)
	return plan, nil
}

// Install 执行已确认的安装计划。
func Install(ctx context.Context, plan InstallPlan) error {
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()

	output, err := runSkillsCommand(runCtx, plan.Command)
	if err != nil {
		return fmt.Errorf("failed to install skills: %w\nmanual fix: %s\n%s",
			err, strings.Join(plan.Command, " "), trimOutput(strings.TrimSpace(output)))
	}
	return nil
}
```

- [ ] **Step 4: 跑测试确认通过**

Run（禁用沙箱）: `go test ./internal/skillsync/ -run 'TestInstall|TestPlanInstall' -v`
Expected: 全部 PASS。

- [ ] **Step 5: 全包门禁**

Run（禁用沙箱）: `make vet && make test`
Expected: exit 0。

- [ ] **Step 6: 更新 `internal/skillsync/CLAUDE.md`**

成员清单追加：

- `install.go`: 安装层——两阶段：PlanInstall（EnsureNpx 门禁 → fetchRemoteSkills 校验按名/展开 --all → InstallCommand 构造命令）产出 InstallPlan 供 cmd 层确认展示；Install 执行计划；按名拼错报错列可用名字，远端不可达降级 Warning 放行；--all 复用 SkillsCommand（与 update 同一命令），按名走上游 -s 多值 + -a '*'
- `install_test.go`: 覆盖 InstallCommand 构造（按名/--all）、PlanInstall（校验通过/拼错列候选/远端不可达降级/--all 展开/--all 降级/缺 npx 不触网）、Install（执行计划命令/失败含 manual fix）；stubNpxPresent + stubRemoteAPI + stubRunSkillsCommand 隔离

- [ ] **Step 7: 提交**

```bash
git add internal/skillsync/install.go internal/skillsync/install_test.go internal/skillsync/CLAUDE.md
git commit -m "feat(skillsync): 两阶段安装层 PlanInstall/Install"
```

---

### Task 3: `cmd/skills_install.go` — install 子命令与确认交互

**Files:**
- Create: `cmd/skills_install.go`
- Create: `cmd/skills_install_test.go`
- Modify: `cmd/skills.go`（挂载 install 子命令 + 头部更新）
- Modify: `cmd/CLAUDE.md`、根 `CLAUDE.md`

**Interfaces:**
- Consumes: `skillsync.PlanInstall(ctx, names, all) (skillsync.InstallPlan, error)`、`skillsync.Install(ctx, plan) error`、`skillsync.InstallPlan{Names, All, Command, Warning}`、`skillsync.SkillsSource`（Task 2）。
- Produces: `newSkillsInstallCmd() *cobra.Command`；包级打桩变量 `planInstallFunc` / `installSkillsFunc` / `confirmInstallFunc`。

- [ ] **Step 1: 写失败测试 `cmd/skills_install_test.go`**

```go
/**
 * [INPUT]: 依赖 bytes、context、errors、strings、testing、internal/skillsync
 * [OUTPUT]: 覆盖 skills install 的互斥校验/无参报错/确认流/-y 跳过/警告出 stderr/错误透传/非 TTY 门控/子命令注册
 * [POS]: cmd/skills install 子命令测试；planInstallFunc / installSkillsFunc / confirmInstallFunc 打桩隔离网络、npx 与终端交互
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

// installStubs 汇集三个打桩点的调用记录。
type installStubs struct {
	planNames    []string
	planAll      bool
	planCalls    int
	confirmCalls int
	installCalls int
	installPlan  skillsync.InstallPlan
}

// stubInstall 打桩 planInstallFunc / confirmInstallFunc / installSkillsFunc。
func stubInstall(t *testing.T, plan skillsync.InstallPlan, planErr, confirmErr, installErr error) *installStubs {
	t.Helper()
	rec := &installStubs{}

	origPlan := planInstallFunc
	planInstallFunc = func(_ context.Context, names []string, all bool) (skillsync.InstallPlan, error) {
		rec.planCalls++
		rec.planNames = names
		rec.planAll = all
		return plan, planErr
	}
	origConfirm := confirmInstallFunc
	confirmInstallFunc = func(skillsync.InstallPlan) error {
		rec.confirmCalls++
		return confirmErr
	}
	origInstall := installSkillsFunc
	installSkillsFunc = func(_ context.Context, p skillsync.InstallPlan) error {
		rec.installCalls++
		rec.installPlan = p
		return installErr
	}
	t.Cleanup(func() {
		planInstallFunc = origPlan
		confirmInstallFunc = origConfirm
		installSkillsFunc = origInstall
	})
	return rec
}

// runInstallCmd 构造并执行 install 子命令，返回 stdout/stderr 与错误。
func runInstallCmd(args ...string) (string, string, error) {
	cmd := newSkillsInstallCmd()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), errOut.String(), err
}

func TestSkillsInstallByName(t *testing.T) {
	plan := skillsync.InstallPlan{Names: []string{"makedsl", "makeui"}}
	rec := stubInstall(t, plan, nil, nil, nil)

	out, _, err := runInstallCmd("makedsl", "makeui", "-y")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if rec.planAll || !slices.Equal(rec.planNames, []string{"makedsl", "makeui"}) {
		t.Fatalf("unexpected plan args: names=%v all=%v", rec.planNames, rec.planAll)
	}
	if rec.installCalls != 1 {
		t.Fatalf("expected 1 install call, got %d", rec.installCalls)
	}
	if !strings.Contains(out, "Installed: makedsl, makeui") {
		t.Fatalf("missing success output:\n%s", out)
	}
}

func TestSkillsInstallAll(t *testing.T) {
	plan := skillsync.InstallPlan{All: true, Names: []string{"makedsl", "makeui"}}
	rec := stubInstall(t, plan, nil, nil, nil)

	out, _, err := runInstallCmd("--all", "-y")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !rec.planAll || len(rec.planNames) != 0 {
		t.Fatalf("unexpected plan args: names=%v all=%v", rec.planNames, rec.planAll)
	}
	if !strings.Contains(out, "Installed: makedsl, makeui") {
		t.Fatalf("missing success output:\n%s", out)
	}
}

func TestSkillsInstallAllUnknownList(t *testing.T) {
	// --all 且远端不可达：Names 为空，成功输出退化为固定句。
	rec := stubInstall(t, skillsync.InstallPlan{All: true}, nil, nil, nil)

	out, _, err := runInstallCmd("--all", "-y")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if rec.installCalls != 1 {
		t.Fatalf("expected 1 install call, got %d", rec.installCalls)
	}
	if !strings.Contains(out, "Installed all Make platform skills") {
		t.Fatalf("missing success output:\n%s", out)
	}
}

func TestSkillsInstallAllWithNamesRejected(t *testing.T) {
	rec := stubInstall(t, skillsync.InstallPlan{}, nil, nil, nil)

	_, _, err := runInstallCmd("--all", "makedsl")
	if err == nil || !strings.Contains(err.Error(), "cannot use --all with skill names") {
		t.Fatalf("expected mutual-exclusion error, got: %v", err)
	}
	if rec.planCalls != 0 {
		t.Fatalf("plan must not run, got %d calls", rec.planCalls)
	}
}

func TestSkillsInstallRequiresNamesOrAll(t *testing.T) {
	rec := stubInstall(t, skillsync.InstallPlan{}, nil, nil, nil)

	_, _, err := runInstallCmd()
	if err == nil || !strings.Contains(err.Error(), "--all") {
		t.Fatalf("expected usage error, got: %v", err)
	}
	if rec.planCalls != 0 {
		t.Fatalf("plan must not run, got %d calls", rec.planCalls)
	}
}

func TestSkillsInstallConfirmThenInstall(t *testing.T) {
	rec := stubInstall(t, skillsync.InstallPlan{Names: []string{"makedsl"}}, nil, nil, nil)

	if _, _, err := runInstallCmd("makedsl"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if rec.confirmCalls != 1 || rec.installCalls != 1 {
		t.Fatalf("expected confirm=1 install=1, got confirm=%d install=%d", rec.confirmCalls, rec.installCalls)
	}
}

func TestSkillsInstallConfirmDeclineShortCircuits(t *testing.T) {
	rec := stubInstall(t, skillsync.InstallPlan{Names: []string{"makedsl"}}, nil, errors.New("install cancelled"), nil)

	_, _, err := runInstallCmd("makedsl")
	if err == nil || !strings.Contains(err.Error(), "cancelled") {
		t.Fatalf("expected cancellation error, got: %v", err)
	}
	if rec.installCalls != 0 {
		t.Fatalf("install must not run after decline, got %d calls", rec.installCalls)
	}
}

func TestSkillsInstallYesSkipsConfirm(t *testing.T) {
	rec := stubInstall(t, skillsync.InstallPlan{Names: []string{"makedsl"}}, nil, errors.New("must not be called"), nil)

	if _, _, err := runInstallCmd("makedsl", "-y"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if rec.confirmCalls != 0 {
		t.Fatalf("confirm must be skipped with -y, got %d calls", rec.confirmCalls)
	}
}

func TestSkillsInstallWarningGoesToStderr(t *testing.T) {
	plan := skillsync.InstallPlan{Names: []string{"makedsl"}, Warning: "cannot verify skill names"}
	stubInstall(t, plan, nil, nil, nil)

	out, errOut, err := runInstallCmd("makedsl", "-y")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(errOut, "warning: cannot verify skill names") {
		t.Fatalf("warning missing from stderr:\n%s", errOut)
	}
	if strings.Contains(out, "warning:") {
		t.Fatalf("warning must not pollute stdout:\n%s", out)
	}
}

func TestSkillsInstallPlanErrorPropagates(t *testing.T) {
	rec := stubInstall(t, skillsync.InstallPlan{}, errors.New("unknown Make platform skills: nope"), nil, nil)

	_, _, err := runInstallCmd("nope", "-y")
	if err == nil || !strings.Contains(err.Error(), "unknown Make platform skills") {
		t.Fatalf("expected plan error, got: %v", err)
	}
	if rec.installCalls != 0 {
		t.Fatalf("install must not run on plan error, got %d calls", rec.installCalls)
	}
}

func TestSkillsInstallErrorPropagates(t *testing.T) {
	stubInstall(t, skillsync.InstallPlan{Names: []string{"makedsl"}}, nil, nil, errors.New("failed to install skills"))

	_, _, err := runInstallCmd("makedsl", "-y")
	if err == nil || !strings.Contains(err.Error(), "failed to install skills") {
		t.Fatalf("expected install error, got: %v", err)
	}
}

func TestConfirmInstallNonInteractiveRejects(t *testing.T) {
	// go test 环境 stdin 非 TTY，真门控直接生效。
	err := confirmInstall(skillsync.InstallPlan{Names: []string{"makedsl"}})
	if err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected non-interactive rejection hinting --yes, got: %v", err)
	}
}

func TestSkillsGroupHasInstall(t *testing.T) {
	for _, sub := range newSkillsCmd().Commands() {
		if sub.Name() == "install" {
			return
		}
	}
	t.Fatal("skills group missing install subcommand")
}
```

- [ ] **Step 2: 跑测试确认失败**

Run（禁用沙箱）: `go test ./cmd/ -run 'TestSkillsInstall|TestConfirmInstall|TestSkillsGroupHasInstall' -v`
Expected: FAIL，`undefined: newSkillsInstallCmd` 等编译错误。

- [ ] **Step 3: 实现 `cmd/skills_install.go`**

```go
/**
 * [INPUT]: 依赖 context、errors、fmt、os、strings、charm.land/huh/v2、github.com/mattn/go-isatty、github.com/spf13/cobra、internal/skillsync
 * [OUTPUT]: 对外提供 newSkillsInstallCmd 函数；包级 planInstallFunc / installSkillsFunc / confirmInstallFunc 可打桩变量
 * [POS]: cmd/skills 的 install 子命令：按名选装 / --all 全量互斥，缺省 huh confirm 确认（--yes 跳过，非 TTY 拒绝并指引），两阶段调用 skillsync.PlanInstall → Install
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

// planInstallFunc / installSkillsFunc / confirmInstallFunc 为包级可打桩变量，
// 单测替换以隔离网络、npx 执行与终端交互（参照 skills_remove.go removeSkillsFunc 模式）。
var planInstallFunc = skillsync.PlanInstall
var installSkillsFunc = skillsync.Install
var confirmInstallFunc = confirmInstall

func newSkillsInstallCmd() *cobra.Command {
	var all, yes bool

	cmd := &cobra.Command{
		Use:   "install [name]...",
		Short: "Install Make platform skills",
		Example: `  makecli skills install makedsl makeui    # 按名选装
  makecli skills install --all             # 全量安装（装缺的 + 升级已有）
  makecli skills install --all -y          # 跳过确认（CI / 非交互）`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillsInstall(cmd.Context(), cmd, args, all, yes)
		},
	}

	cmd.Flags().BoolVarP(&all, "all", "a", false, "install all Make platform skills")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	return cmd
}

func runSkillsInstall(ctx context.Context, cmd *cobra.Command, names []string, all, yes bool) error {
	if all && len(names) > 0 {
		return errors.New("cannot use --all with skill names")
	}
	if !all && len(names) == 0 {
		return errors.New("specify skill names or --all (run 'makecli skills list' to see what's available)")
	}

	plan, err := planInstallFunc(ctx, names, all)
	if err != nil {
		return err
	}
	if plan.Warning != "" {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", plan.Warning)
	}

	if !yes {
		if err := confirmInstallFunc(plan); err != nil {
			return err
		}
	}

	if err := installSkillsFunc(ctx, plan); err != nil {
		return err
	}

	if len(plan.Names) > 0 {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Installed: %s\n", strings.Join(plan.Names, ", "))
	} else {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Installed all Make platform skills")
	}
	return nil
}

// confirmInstall 在执行前确认安装计划（deploy production 同款 huh confirm 护栏）。
// 非交互终端（管道 / CI）无法确认，直接拒绝并指引 --yes，杜绝挂起。
func confirmInstall(plan skillsync.InstallPlan) error {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return errors.New("refusing to install without confirmation: re-run with --yes in a non-interactive shell")
	}

	list := "all skills"
	if len(plan.Names) > 0 {
		list = strings.Join(plan.Names, ", ")
	}

	confirmed := false
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Install Make platform skills?").
				Description(fmt.Sprintf("Source: %s\nSkills: %s\nTarget: all detected code agents",
					skillsync.SkillsSource, list)).
				Affirmative("Install").
				Negative("Abort").
				Value(&confirmed),
		),
	).Run()

	if errors.Is(err, huh.ErrUserAborted) || (err == nil && !confirmed) {
		return errors.New("install cancelled")
	}
	return err
}
```

- [ ] **Step 4: 挂载到 `cmd/skills.go`**

`newSkillsCmd` 的 AddCommand 序列加一行（放在 list 之后）：

```go
	cmd.AddCommand(newSkillsListCmd())
	cmd.AddCommand(newSkillsInstallCmd())
	cmd.AddCommand(newSkillsUpdateCmd())
	cmd.AddCommand(newSkillsRemoveCmd())
```

同步更新 skills.go 头部 `[POS]`：挂载 list / install / update / remove 子命令。

- [ ] **Step 5: 跑测试确认通过**

Run（禁用沙箱）: `go test ./cmd/ -run 'TestSkillsInstall|TestConfirmInstall|TestSkillsGroupHasInstall' -v`
Expected: 全部 PASS。

- [ ] **Step 6: 全量门禁**

Run（禁用沙箱）: `make vet && make test && golangci-lint run ./...`
Expected: 全部 exit 0、0 issues。

- [ ] **Step 7: 更新文档**

`cmd/CLAUDE.md` 成员清单：

- 追加 `skills_install.go`: skills install 子命令——按名选装 / --all 全量互斥（都缺报错指引 skills list），两阶段 skillsync.PlanInstall（npx 门禁 + 远端校验）→ huh confirm 确认（--yes 跳过；非 TTY 拒绝指引 --yes，deploy production 同款护栏）→ skillsync.Install；plan.Warning 渲染 stderr；planInstallFunc / installSkillsFunc / confirmInstallFunc 包级可打桩变量
- 追加 `skills_install_test.go`: 覆盖 skills install 的互斥/无参报错/确认流（先 confirm 后 install、拒绝短路、-y 跳过）/警告出 stderr/plan 与 install 错误透传/非 TTY 真门控/子命令注册，stubInstall 三点打桩隔离
- 修订 `skills.go` 行：挂载 list / install / update / remove 子命令

根 `CLAUDE.md` `<directory>` 两处修订：

- `cmd/` 行：`skills[list/update/remove]` → `skills[list/install/update/remove]`
- `internal/skillsync/` 行：描述加上「按名/全量安装（PlanInstall 两阶段 + EnsureNpx npx 环境门禁，门禁盖 Sync/Remove/Install 三动词）」

- [ ] **Step 8: 提交**

```bash
git add cmd/skills_install.go cmd/skills_install_test.go cmd/skills.go cmd/CLAUDE.md CLAUDE.md
git commit -m "feat(skills): install 子命令——按名/--all 选装 + 确认护栏 + npx 门禁"
```

---

### Task 4: 收尾门禁与冒烟

**Files:**
- 无新文件；只跑验证。

**Interfaces:**
- Consumes: Task 1–3 全部产物。
- Produces: 可发布状态（不 push——发布走 /ship）。

- [ ] **Step 1: 全量验证**

Run（禁用沙箱）: `make vet && make test && golangci-lint run ./...`
Expected: exit 0、0 issues。

- [ ] **Step 2: 构建冒烟**

Run（禁用沙箱）: `make build && bin/makecli skills install --help && bin/makecli skills --help`
Expected: help 正常渲染，install 出现在 skills 子命令列表，flags 含 `-a, --all` 与 `-y, --yes`。

- [ ] **Step 3: 非交互门控冒烟**

Run: `echo | bin/makecli skills install makedsl 2>&1 | head -5`（管道使 stdin 非 TTY）
Expected: 报错含 `re-run with --yes`（若本机可达 GitHub 则先过名字校验；离线则先见 warning 再见拒绝——两者都算通过）。

- [ ] **Step 4: 确认工作区干净**

Run: `git status --short`
Expected: 无未提交变更（`agent/`、`skills-lock.json` 两个既有未跟踪项除外，不属于本计划）。
