# makecli skills install 命令设计

日期：2026-07-28
状态：已确认（JimYu 2026-07-28）

## 目标

补齐 `skills` 命令族的安装动词：`skills list` 能看到 `not installed` 的 skill，但当前没有按名安装的入口（只有 `skills update` 全量拉）。`skills install` 提供按名选装与 `--all` 全量两种形态，安装前有确认步与 npx 环境前置检查。

## 背景：多 agent 分发的归属

把 skill 装进 Claude Code / Codex / Cursor 等多个 code agent 的目录，这件事由上游 `vercel-labs/skills` CLI（`npx skills`）负责：正本落在 `~/.agents/skills/`（或项目级 `.agents/skills/`），向每个检测到的 agent 目录打符号链接（如 `~/.claude/skills/<name> -> ../../.agents/skills/<name>`），`skills-lock.json` 记录 source + hash。makecli 的 `skills update` / `remove` / `install` 都是它的编排层——makecli 不自建 agent 注册表、不自己写 agent 目录。

## 命令结构

```
makecli skills install --all [-y]          # 全量安装（装缺的 + 升级已有）
makecli skills install [-y] <name>...      # 按名选装
```

- `-a|--all` 与位置参数互斥（报错 `cannot use --all with skill names`）；两者都缺 → 报错提示用法。
- `-y|--yes` 跳过确认。
- 底层命令映射（npx 恒以非交互执行——`runSkillsCommand` 把 stdout/stderr 捕获进 buffer，上游交互提示会挂死）：
  - `--all` → `npx -y skills add qfeius/make-platform-skills --all`（上游 `--all` = `--skill '*' --agent '*' -y`）
  - 按名 → `npx -y skills add qfeius/make-platform-skills -s <name1> <name2> ... -a '*' -y`
- 上游 `-s` 的传参形态已验证（vercel-labs/skills `parseAddOptions`）：单个 `-s` 后贪婪收集空格分隔多值，直到下一个 `-` 开头参数。`-a '*'` 经 exec 直传不过 shell，无 glob 展开问题。

## 确认交互

缺省（无 `-y`）在执行前确认，与 `app delete` / `deploy production` 同款护栏：

- TTY：展示 source（`qfeius/make-platform-skills`）、目标（所有检测到的 code agent）、将安装的 skill 清单，`charm.land/huh` confirm 放行；拒绝即取消，不触 npx。
  - 按名安装：清单 = 校验后的名字。
  - `--all`：清单 = 远端 skill 列表（`fetchRemoteSkills` 可达时），不可达时显示 `all skills`。
- 非 TTY 且无 `-y`：拒绝并指引 `--yes`（go-isatty 检测，杜绝 CI 挂起）。
- `confirmInstallFunc` 包级可打桩变量隔离终端交互。

## 按名校验

对照 `fetchRemoteSkills`（GitHub Contents API，inventory.go 已有）校验传入名字：

- 名字不在远端清单 → 报错并列出可用 skill 名（与 `remove` 用 lockfile 校验对称）。
- 远端不可达 → 降级放行（stderr 警告），由 npx 裁决——校验是增强，不是门禁。

## npx 环境前置检查

`EnsureNpx()`：`exec.LookPath("npx")` 缺失时不让 exec 报晦涩错误，输出面向 agent 一步收敛的指引（preflight How-to-fix 风格）：

```
npx not found: Make platform skills are distributed via the 'skills' npm CLI.
How to fix:
  macOS:  brew install node
  or install Node.js (npx ships with npm): https://nodejs.org
Then re-run: makecli skills install ...
```

放在 `internal/skillsync` 共享层，`Sync` / `Remove` / `Install` 三个动词统一在 shell out 前调用——它们同样依赖 npx，缺 npx 时失败信息里的 `manual fix: npx ...` 本身也没法跑。不给 install 开特例。

## 代码落位

```
internal/skillsync/
  env.go             # 新增：EnsureNpx()（LookPath seam 可打桩）
  install.go         # 新增：Install(ctx, Options{Names, All})——远端校验 → 构造命令 → runSkillsCommand
  sync.go            # Sync 前置 EnsureNpx
  remove.go          # Remove 前置 EnsureNpx
cmd/
  skills.go          # 挂载 install 子命令
  skills_install.go  # 新增：flag 解析、互斥校验、确认交互、结果渲染
```

复用既有原语：`runSkillsCommand` / `syncTimeout` / `trimOutput`（sync.go）、`fetchRemoteSkills`（inventory.go）。

## 错误处理

- `--all` 与名字并存、两者都缺 → 用法错误。
- 名字不在远端清单 → 报错列出可用名字。
- 远端清单拉取失败（按名模式）→ stderr 警告 + 放行。
- 缺 npx → EnsureNpx 指引文案，退出码 1。
- 非 TTY 无 `-y` → 拒绝并指引 `--yes`。
- npx 执行失败 → 与 Sync 同款：报错 + `manual fix:` 可复制命令 + 截断输出。

## 测试

沿用邻居模式：

- `installSkillsFunc`（cmd 层）/ `runSkillsCommand`（skillsync 层）打桩隔离 npx。
- `stubRemoteAPI`（inventory_test.go 已有）隔离 GitHub。
- `confirmInstallFunc` 打桩隔离终端交互。
- `EnsureNpx` 经 LookPath seam 打桩测缺失路径。
- 覆盖：互斥校验 / 无参报错 / 确认拒绝在触 npx 前短路 / 非 TTY 门控 / 名字校验失败列候选 / 远端不可达降级 / `--all` 与按名的命令构造 / 缺 npx 指引。

## 明确不做（YAGNI）

- `--agent` / `--scope` / `--global` 旋钮：沿用上游 `-a '*'` + `-y` auto-detect scope，`update` 同款行为。
- `@version` 钉版本（skills 没有 semver，hash 语义由 lockfile 承载）。
- 自建 agent 注册表 / 自己写 agent 目录。
- 交互式选单（无参进入挑选列表）：无参即报错，保持非交互友好。
