# makecli 自动更新提示（update notifier）设计

> 日期: 2026-05-29 · 作者: JimYu + Claude · 状态: 已批准设计，待写实现计划

## 1. 目标

`makecli` 任意命令执行时，自动、低成本地检测是否有更新版本；若有，在命令结束后向用户提示一行升级指引（`makecli update`）。

**核心约束**：绝不拖慢主命令、绝不污染 stdout、绝不在自动化/CI 场景制造噪音。

## 2. 架构总览

新增 `internal/notifier` 包，单一职责：**判断要不要提示 + 维护检测缓存**。消费已有的 `update.CheckLatest`，被 `cmd.Execute()` 在头尾两处钩入。

```
cmd.Execute()
  ├─ n := notifier.Start()      // 缓存过期才起 goroutine 并行刷新缓存
  ├─ err := rootCmd.Execute()   // 命令本体照常执行（goroutine 在后台跑）
  └─ defer n.Finish(cmdName)    // 极短 deadline 收尾 → 读最新缓存 → 决定是否打印提示
```

**为什么放 `Execute()` 包裹层**：`PersistentPreRun/PostRun` 会被子命令覆盖且不保证执行；包裹层对所有命令统一生效，零侵入子命令。

### 复用点

| 复用 | 来源 |
|------|------|
| `CheckLatest(current) (*Release, newer, err)` | `internal/update` |
| `config.Dir()` → 缓存落点 | `internal/config` |
| `build.Version` → 当前版本 | `internal/build` |
| `SetAPIBaseURLForTest` → 测试隔离网络 | `internal/update` |

## 3. 缓存文件

- 路径：`<config.Dir()>/update-check.json`（默认 `~/.make/update-check.json`，自动跟随 `$MAKE_CLI_CONFIG_DIR`）
- 结构：
  ```json
  {
    "checked_at": "2026-05-29T10:00:00Z",
    "latest_version": "v0.3.0",
    "html_url": "https://github.com/qfeius/makecli/releases/tag/v0.3.0"
  }
  ```
- 读写一律 **best-effort**：文件不存在 / 损坏 / 无权限 → 静默降级为"不提示"，**绝不影响主命令退出码与输出**。

## 4. 刷新机制（goroutine + 短 deadline）

1. `Start()`：读缓存。`checked_at` 距今 < 24h（`checkInterval`）→ 直接返回，不发任何请求。过期 → 起一个 goroutine 调 `update.CheckLatest`，结果写回缓存文件。请求自带独立 HTTP 超时（不依赖主进程存活）。
2. goroutine 与命令本体**并行**：走网络的命令（`record list` 等）天然给了它几百毫秒窗口。
3. `Finish()`：给 goroutine ~250ms（`finishDeadline`）的收尾窗口；到点未完成就放弃（缓存这次没填上，下次命令再填）。然后读缓存、判定、打印。

**可靠性铁律**：goroutine 内 `recover` 兜底 panic；HTTP 请求设 `context.WithTimeout`。任何 notifier 内部错误都不得冒泡到主流程。

## 5. 启用开关与优先级（三态）

是否启用更新提示，按优先级从高到低裁决：

| 优先级 | 来源 | 语义 |
|--------|------|------|
| 1（最高） | env `MAKE_CLI_UPDATE_NOTIFIER` | `true/1/yes` → 强制开；`false/0/no` → 强制关；**未设置则下沉** |
| 2 | config `[settings] check-for-updates` | `true`/`false`；未设置则下沉 |
| 3（默认） | 内置 | **开启** |

env 显式设值覆盖一切（双向），config 提供持久默认，二者都没设则默认开。解析为纯函数 `notifierEnabled(env, cfgVal) bool`。

### config 全局段

当前 `config` 文件是纯 per-profile 结构（每个 `[section]` 即一个 profile），`parseConfigINI` 丢弃无 section 头的键，**不支持全局项**。更新开关与 profile/凭证无关，是**全局配置**，因此引入保留段 `[settings]`：

```ini
[settings]
check-for-updates = false

[default]
server-url = ...
```

config 包改动（实现阶段细化）：把 INI 解析下沉为"通用 section → kv map"的底层 parser，`LoadConfig`（profiles）与新增 `LoadSettings`（全局段）各自从同一份解析结果投影——消除两套扫描器，单一数据源。`LoadSettings` best-effort：文件缺失/损坏返回空，由调用方按默认值处理。

## 5b. 是否提示的判定链（任一不满足即静默）

在「启用」为 true 的前提下，仍需逐条满足：

| 条件 | 静默原因 |
|------|---------|
| `build.Version == "DEV"` 或非法 semver | 开发态没有"更新"概念 |
| `os.Getenv("CI") != ""` | CI 环境（默认 CI 不设值即不触发） |
| `stderr` 不是 TTY | 管道 / 重定向 / 被捕获 |
| 当前命令 ∈ {`version`, `update`, `help`, `completion`} | 命令本身已处理版本，避免重复/打架 |
| 缓存 `latest_version <= 当前版本`（semver 比较） | 没有更新 |

判定全部为纯函数 `shouldNotify(...)`，便于穷举单测。

## 6. 提示文案（输出到 stderr）

```
─────────────────────────────────────────────
 A new release of makecli is available: 0.2.0 → 0.3.0
 To upgrade, run: makecli update
 https://github.com/qfeius/makecli/releases/tag/v0.3.0
─────────────────────────────────────────────
```

## 7. 测试策略

- `internal/notifier`：
  - `shouldNotify`：穷举 version / 各 env / cache 组合（含 DEV、关闭开关、CI、非新版本、正常提示）。
  - 缓存读写 + 过期判定：用 `t.Setenv(MAKE_CLI_CONFIG_DIR, tmp)` 隔离文件系统。
  - 刷新流程：`update.SetAPIBaseURLForTest(httptest)` 隔离网络。
- TTY 检测抽成可注入的接口/变量，单测可强制 true/false。
- 不碰真实网络、不碰真实 `~/.make`。

## 8. 配置项汇总（默认值）

| 项 | 默认值 |
|----|--------|
| `checkInterval` | 24h |
| `finishDeadline` | 250ms |
| 缓存文件 | `<config.Dir()>/update-check.json` |
| 启用开关环境变量 | `MAKE_CLI_UPDATE_NOTIFIER`（三态 true/false，未设下沉） |
| 启用开关 config 项 | `[settings] check-for-updates`（未设下沉，默认开） |
| CI 静默环境变量 | `CI`（非空即静默） |

## 9. 文档同步（GEB）

- 新增 `internal/notifier/CLAUDE.md`（L2）。
- 更新根 `CLAUDE.md` 的 `<directory>` 加入 `internal/notifier/`。
- 更新 `cmd/CLAUDE.md` 的 `root.go` 行（Execute 钩入 notifier）。
- 更新 `internal/config/CLAUDE.md`：`config.go` 支持 `[settings]` 全局段、新增 `settings.go`（或在 config.go 内）的 `LoadSettings`。
- 各新文件带 L3 头部契约。

## 10. 非目标（YAGNI）

- 不做后台分离子进程（已选 goroutine 方案）。
- 不自动执行更新（提示而非代劳，尊重用户）。
- `[settings]` 段当前只承载 `check-for-updates` 一项，不预先扩充其它全局配置。
