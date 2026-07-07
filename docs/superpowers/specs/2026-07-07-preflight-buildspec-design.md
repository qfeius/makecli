# preflight 对齐 build spec 检查清单 — 设计

日期：2026-07-07
规范依据：`make-build-service` 的 `build_spec.md`（第 5 节检查清单，快照见文末附录）

## 背景与定位

现有 `makecli preflight` 只按 `--app-type`（fullstack/service/ui）对
`apps/dsl`、`apps/service/package.json`、`apps/ui/package.json` 做 stat 检查。
build spec 第 5 节定义了完整的构建可行性检查清单，且规范原文写明
「makecli preflight 以第 5 节检查清单为实现依据」。

**定位决策：完全替换。** preflight 以 spec 第 5 节为唯一实现依据：

- 构建模式 A/B 按 spec 第 2 节**自动判定**（`apps/ui/package.json` 或
  `apps/service/package.json` 任一存在 → 模式 A，否则模式 B），移除 `--app-type`。
- `apps/dsl` 检查保留为模式 A 下的一项 makecli 自有条目（编号 D1，ERROR）——
  Make-app 身份核心，deploy 的 `appKeyFromDSL` 依赖它，build spec 不关心但 makecli 关心。
  D1 不止查目录存在：`apps/dsl/app.yaml` 必须可解析且 Make.App key 非空，
  否则目录在但 key 读不出来时会出现「preflight 绿、deploy 红」的缺口。
- 包管理器按 spec 第 1 节 lockfile 优先级判定（pnpm > yarn > npm，检测目录
  模式 A 为 `apps/`、模式 B 为仓库根）。

## 检查范围（本版）

**只做确定性检查，零误报。** BUILD-TIME / 启发式条目（A10、A12、A13、A14、B4）
整体缓实现，后续按真实误报/漏报案例再补。

| 组 | 条目 |
|---|---|
| 通用 | G1（repoName 正则）、G2（INFO：构建 30 分钟上限）、P1（多 lockfile WARN） |
| 模式 A | A1–A9、A11、A15（TEMP：service × yarn/npm 拦截）+ D1（`apps/dsl/` 存在且 `app.yaml` 可读、Make.App key 非空，ERROR） |
| 模式 B | B1–B3 |

各条目语义严格以 build spec 第 5 节表格为准，不在本文复述。两处 makecli 侧的具体化：

- **G1 repoName 来源**：`apps/dsl/app.yaml` 存在时取 app key
  （deploy 建仓即 `CreateRepository(appKey)`，远端仓库名由 appKey 派生）；
  否则回退工作目录 basename。对 `lower(repoName)` 施加
  `^[a-z0-9]+([._-][a-z0-9]+)*$`，输出注明实际检查的名字来源。
- **A8 不受模式限制**：spec 条件列写「模式 A」，但第 7 节首行要求
  「组件目录存在但无 package.json 且回退模式 B」时同时命中 A8 + B1——
  故 A8 的触发条件实现为「`apps/ui/` 或 `apps/service/` 目录存在而无
  `package.json`」，与模式无关（这正是「你可能想用模式 A」的关键警告）。
- **A4/A5 workspace 覆盖判定**：pnpm 的 `packages` 与 yarn/npm 的 `workspaces`
  条目按 `path.Match` glob 匹配组件目录名，兼容 `ui`、`./ui`、`ui/`、`*` 等写法；
  yarn/npm 的 `workspaces` 兼容数组与 `{packages: [...]}` 两种形态。

## 架构：上下文 + 表驱动

一次性构建 `preflightContext`（模式、包管理器、发现的 lockfile 列表、解析好的
`apps/package.json` / 组件 `package.json` / `pnpm-workspace.yaml`、repoName），
然后一张检查表按 spec 顺序求值：

```go
type check struct {
    id      string                          // 与 spec 条目 1:1，如 "A3"
    level   level                           // ERROR / WARN / INFO
    applies func(*preflightContext) bool    // spec 的「条件」列
    run     func(*preflightContext) result  // spec 的「判定」列
}
```

- 检查与 spec 条目 1:1 对应，日后删 A15（差距关闭）只删表中一行。
- `applies` 为假的条目跳过且不打印；`run` 返回 pass 或带原因的 fail。
- 文件解析失败（如 package.json 非法 JSON）计入对应条目的 fail 原因，不 panic。

## 输出与退出码

输出语言为英文（与 makecli 其余命令一致）。

```
Project:          /path/to/repo
Mode:             A (apps components)
Package manager:  pnpm

✓ D1 apps/dsl/
✓ A1 apps/package.json
✗ A3 [ERROR] no lockfile in apps/ (need one of pnpm-lock.yaml / yarn.lock / package-lock.json)
✗ A6 [ERROR] apps/ui/package.json name is "frontend", must be "ui"
! P1 [WARN]  multiple lockfiles found; pnpm-lock.yaml wins (pnpm > yarn > npm), others ignored
i G2 [INFO]  build job time limit is 30 minutes by default

FAIL: 2 errors, 1 warning
```

- 存在 ERROR → 返回既有哨兵 `errPreflightFailed`（main.go 转译退出码 1）。
- 仅 WARN/INFO → exit 0。不设 `--strict`（YAGNI）。
- 保留 `[dir]` 位置参数，默认 cwd。

## 失败输出面向 AI agent 的可操作性

preflight 的核心消费场景之一是 AI coding agent：跑 preflight → 按输出修 repo →
重跑直到通过。失败输出必须让 agent **不读 build spec 原文也能一步收敛**，因此：

1. **每条检查自带 fix 指引**：`check` 结构体增加
   `fix func(*preflightContext) string`，失败消息本身给出「实际值 vs 期望值」
   （如 A6 报出当前 name 是什么），fix 给出具体动作，并按上下文插值
   （如 A3 的 fix 按已判定的包管理器给出对应命令）。
2. **失败时追加 "How to fix" 块**，逐条列出 ERROR/WARN 的修复动作：

```
FAIL: 2 errors, 1 warning

How to fix:
  A3  service images reinstall dependencies with a frozen lockfile. Run
      `pnpm install` inside apps/ and commit pnpm-lock.yaml.
  A6  set "name": "ui" in apps/ui/package.json — the build system locates
      components by package name, not by path.
  P1  keep exactly one lockfile in apps/; delete yarn.lock.
```

   fix 文案三要素：为什么（一句话，构建系统的行为依据）、改哪个文件、改成什么。
   通过的检查不打印 fix，保持输出紧凑。
3. **不做 `--output json`**：agent 在 shell 里直接读 stdout 文本即可，
   结构化文本（固定前缀 `✗/!/i` + 检查 ID + How to fix 块）对 LLM 已足够友好；
   有 CI 程序化解析需求时再加。

## 文件与测试

- 重写 `cmd/preflight.go`：上下文构建 + 检查表 + 渲染，单文件。
- 重写 `cmd/preflight_test.go`：`t.TempDir` 构造目录树，覆盖
  spec 第 7 节「常见失败结构速查」9 行中的 8 行场景（第 7 行为 BUILD-TIME
  启发式，本版缓实现）、包管理器判定优先级
  （含多 lockfile）、模式判定边界（组件目录存在但无 package.json → A8 + 回退模式 B）、
  G1 两种 repoName 来源、A4/A5 glob 各形态、退出码语义。
- 依赖零新增（yaml、encoding/json 均已在用）。
- 同步更新：preflight.go 头部注释、命令 Long 帮助、`cmd/CLAUDE.md` 成员清单。

## 不做的事

- BUILD-TIME 启发式（A10/A12/A13/A14/B4）。
- 跑外部命令的 lockfile 深度同步检查。
- `--output json`（现有 preflight 也没有；有 CI 解析需求时再加）。
- `--app-type` 兼容层：直接移除，旧 flag 使用者收到 cobra unknown flag 报错，自然迁移。

## 附录：spec 快照要点

实现时以 `/private/tmp/make-build-service/build_spec.md`（本设计定稿时的版本）为准。
关键判定复述：模式判定见 spec 第 2 节；lockfile 优先级见第 1 节；
A15 为 TEMP 条目，对应 build-job.sh commit `1b83199` 的 service 镜像模板仅支持
pnpm 的差距，差距关闭后删除该行即可。
