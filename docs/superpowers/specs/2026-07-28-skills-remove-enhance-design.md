# makecli skills remove 增强设计（--all + 确认护栏）

日期：2026-07-28
状态：已确认（JimYu 2026-07-28）

## 目标

`skills remove` 补齐与 `skills install` 对称的两种形态与护栏：

```
makecli skills remove --all [-y]        # 移除全部已装 Make platform skills
makecli skills remove [-y] <name>...    # 按名移除
```

## `--all` 的安全边界

上游 `npx skills remove --all` 等价 `--skill '*' --agent '*'`，会连第三方 skills 一起删除。makecli 的 `--all` **不透传上游 `--all`**：从 lockfile 读出 `source == qfeius/make-platform-skills` 的已装名单展开成名字，仍以按名形式交给 npx。来源校验这道墙对两种形态一体生效。

## 执行模型：逐个删除

展开/校验后的名单**逐个执行** `npx -y skills remove <name> -y`——每个 skill 一次 npx 调用、独立超时（syncTimeout）、逐项收集结果；单个失败不中断后续，全部执行完后统一汇报（`record delete` 同款批量语义）。

## 命令面

- `-a|--all` 与位置参数互斥（`cannot use --all with skill names`）；两者都缺 → 报错提示用法（与 install 同文案结构）。
- 缺省确认步：展示 source、将移除的 skill 清单、目标（所有检测到的 code agent），huh confirm（Affirmative "Remove" / Negative "Abort"）；拒绝即取消不触 npx。
- `-y|--yes` 跳过确认；非 TTY 且无 `-y` → 拒绝并指引 `--yes`。
- `--all` 且 lockfile 无 Make platform skills → 报错 `no Make platform skills installed`。
- 按名校验沿用现状：名字必须都在 lockfile 的 Make 来源清单内，拼错报错并列出已装候选。

## 结构（对称 install 的两阶段）

```go
type RemovePlan struct {
    Names   []string // 校验/展开后的待删清单（lockfile 为准，已排序）
    All     bool
    Warning string   // lockfile 损坏等降级警告，cmd 层渲染 stderr
}

func PlanRemove(names []string, all bool) (RemovePlan, error)
// EnsureNpx 门禁 → readLock 校验按名 / 展开 --all → 产出计划；纯本地无网络，无 ctx

type RemoveResult struct {
    Name string
    Err  error // nil = 已移除；失败含 manual fix 与截断输出
}

func Remove(ctx context.Context, plan RemovePlan) ([]RemoveResult, error)
// 逐个执行，返回逐项结果；任一失败时 error 汇总 "failed to remove N of M skills"
```

cmd 层 `skills_remove.go` 与 `skills_install.go` 同构：互斥校验 → PlanRemove → Warning 渲染 stderr → confirm（`confirmRemoveFunc` 打桩）→ Remove → 渲染结果。全部成功输出 `Removed: <names>`；部分失败逐行列出失败项（`failed <name>: <err>`）并上抛汇总错误（退出码 1）。

## 顺手收账（终审递延 Minor）

- `Install` 与 `Remove` 执行函数补 doc 注释声明契约：计划必须来自对应 Plan 阶段（EnsureNpx 门禁在 Plan 层）。
- `cmd/skills_install_test.go` 补断言：确认后传给 `installSkillsFunc` 的 plan 与 PlanInstall 产出原样一致（现有 `installPlan` 记录字段从死字段转为真断言）。

## 测试

- skillsync 层：PlanRemove（按名校验/拼错列候选/--all 展开排序/--all 空清单报错/lockfile 警告透传/缺 npx 门禁）；Remove（逐个调用命令构造与次数/部分失败继续执行并汇总/全成功）。stubNpxPresent + stubLockFile + stubRunSkillsCommand 隔离。
- cmd 层：互斥/无参报错、确认流（先 confirm 后执行、拒绝短路、-y 跳过）、警告出 stderr、部分失败渲染与错误上抛、非 TTY 真门控。三点打桩（planRemoveFunc / removeSkillsFunc / confirmRemoveFunc）。

## 明确不做（YAGNI）

- 上游 `--all` 透传（安全边界，永不）。
- 并发删除（逐个串行足够，remove 是低频操作）。
- 进度条/流式输出（与 sync/update 的静默执行一致）。
