# makecli update [version] 命令设计

## 概述

扩展 `makecli update` 接受可选位置参数 `[version]`，允许用户安装任意已发布的 release。默认行为（无 arg）保持不变 — 查 latest 并比较；显式指定版本时走新流程：规范化 → 拉 release → 与当前版本比较 → 按策略决定是否执行。降级（target 比 current 旧）默认拒绝，需 `--force` 覆盖。

## 范围

**包含：**
- `internal/update`：新增 `NormalizeTag` / `GetRelease` / `CompareVersions` 三个导出函数
- `cmd/update.go`：扩展为接受 `[version]` 位置参数 + `--force` 标志，分支处理 latest / specific 两条路径
- `cmd/update_test.go`：新建（当前不存在），覆盖 CLI 层决策逻辑（不实际下载/替换二进制）
- `internal/update/update_test.go`：增量测试新函数
- 通过包级 `applyFunc` 钩子使 `cmd/update.go` 中的 `update.Apply` 调用可在测试中打桩
- 文档：L2（`cmd/CLAUDE.md`、`internal/update/CLAUDE.md`）+ L3 文件头同步

**不包含：**
- `--check` 干跑模式（YAGNI）
- 列出可升级版本（已有 `makecli version list`）
- 自动选择最近 stable（policy 层未来需求，本期不做）
- pre-release 标签过滤（用户显式指定 tag，由其负责）

## 命令行为

```
makecli update                  # 默认行为：latest 流程，无更新则 no-op
makecli update v0.2.20          # 指定版本，升级到 v0.2.20
makecli update 0.2.20           # 等价（自动加 v 前缀）
makecli update v0.2.20 -f       # --force 短形式（即使无降级也允许）
makecli update v0.2.0           # 降级被拒绝，提示用 --force
makecli update v0.2.0 --force   # 显式降级
makecli update v0.2.16          # 同当前版本，no-op 退出
makecli update abc              # 拒绝：not a valid version
makecli update v9.9.9           # 拒绝：release not found on GitHub
```

### Flag

| Flag | 简写 | 含义 |
|------|------|------|
| `--force` | `-f` | 允许降级到比当前版本旧的 release |

### Cobra 配置

```go
Use:   "update [version]"
Args:  cobra.MaximumNArgs(1)
Short: "Update makecli to the latest or a specific version"
SilenceUsage: true
```

## 行为矩阵

| 输入 | 当前版本 | 行为 | 退出码 |
|------|----------|------|--------|
| `update`（无 arg） | latest | "Already up to date (vX)" | 0 |
| `update`（无 arg） | < latest | "Updating makecli: vA → vB" → apply | 0 |
| `update v0.2.20` | v0.2.16 | apply v0.2.20（升级） | 0 |
| `update v0.2.16` | v0.2.16 | "Already at v0.2.16, skipping" | 0 |
| `update v0.2.0` | v0.2.16 | refuse + 提示 | 1 |
| `update v0.2.0 --force` | v0.2.16 | apply v0.2.0（降级） | 0 |
| `update 0.2.20` | v0.2.16 | 规范化到 v0.2.20，按升级路径 | 0 |
| `update abc` | 任意 | reject pre-flight | 1 |
| `update v9.9.9`（tag 不存在） | 任意 | 404 → 明确错误 | 1 |
| `update v0.2.0` | `DEV` / dirty | apply（跳过比较） | 0 |

## 数据层设计（`internal/update`）

### `NormalizeTag(input string) (string, error)`

```go
// "v0.2.0"  → "v0.2.0"
// "0.2.0"   → "v0.2.0"
// "v0.2.0-beta.1" → "v0.2.0-beta.1"
// "abc"     → error
// ""        → error
func NormalizeTag(input string) (string, error)
```

实现：strip leading `v` → `semver.NewVersion(stripped)` 校验 → 返回 `"v" + stripped`。

### `GetRelease(tag string) (*Release, error)`

```go
// 调用 GET {apiBaseURL}/repos/qfeius/makecli/releases/tags/{tag}
// 200 → 返回 *Release
// 404 → 明确错误 "release {tag} not found"
// 其他 → "failed to fetch release {tag}: HTTP {code}"
```

`tag` 必须是 normalized 形式（带 `v` 前缀）。GitHub API 路径要求精确匹配 tag。

### `CompareVersions(target, current string) int`

```go
// 语义：semver 比较 target vs current
//   target > current → 1
//   target == current → 0
//   target < current → -1
//
// 特殊：current 为 DEV 或无效 semver 时返回 1（视为 current "永远旧"，跳过降级保护）
// target 必须已是有效 semver（cmd 层已通过 NormalizeTag 保证）
func CompareVersions(target, current string) int
```

不返回 error：DEV 已是预期场景，与其报错不如「降级保护对 DEV 关闭」更友好。

### `Apply(release *Release)` — 保持不变

已经接受任意 `*Release`，不绑定 latest。新流程直接复用。

## CLI 层设计（`cmd/update.go`）

### 包级 `applyFunc` 钩子

```go
// applyFunc 包装 update.Apply，测试中可打桩避免真实替换二进制
var applyFunc = update.Apply
```

### `newUpdateCmd` 改造

```go
func newUpdateCmd() *cobra.Command {
    var force bool
    cmd := &cobra.Command{
        Use:          "update [version]",
        Short:        "Update makecli to the latest or a specific version",
        Args:         cobra.MaximumNArgs(1),
        SilenceUsage: true,
        RunE: func(cmd *cobra.Command, args []string) error {
            target := ""
            if len(args) == 1 {
                target = args[0]
            }
            return runUpdate(cmd, target, force)
        },
    }
    cmd.Flags().BoolVarP(&force, "force", "f", false, "allow downgrade to an older version")
    return cmd
}
```

### `runUpdate` 分支

```go
func runUpdate(cmd *cobra.Command, target string, force bool) error {
    currentVersion := build.Version

    if target == "" {
        return runUpdateLatest(cmd, currentVersion)
    }
    return runUpdateSpecific(cmd, currentVersion, target, force)
}
```

`runUpdateLatest`：维持现有 `CheckLatest` 流程（仅是抽出来）。

`runUpdateSpecific`：
1. `tag, err := update.NormalizeTag(target)` — 非法直接返回错误
2. `release, err := update.GetRelease(tag)` — 404 直接返回错误
3. 若 `currentVersion` 非 DEV 且有效：`cmp := update.CompareVersions(tag, currentVersion)`
   - `cmp == 0`：打印 "Already at {tag}, skipping" → return nil
   - `cmp < 0 && !force`：return error "v0.2.0 is older than current v0.2.16. Use --force to downgrade."
4. 打印 "Updating makecli: {current} → {tag}"
5. `applyFunc(release)`
6. 打印 "Updated makecli: {current} → {tag}"

### "DEV 视为永远旧" 的实现位置

在 `CompareVersions` 内部 — 当 `current` 解析失败（DEV / dirty / git-describe 输出），直接返回 `1`。这样 cmd 层逻辑统一：`cmp < 0` 才走降级分支，DEV 永远不会走到。

## 错误信息（用户可见）

| 场景 | 信息 |
|------|------|
| 非法 version 输入 | `invalid version "abc": ...semver error...` |
| tag 不存在 | `release v9.9.9 not found` |
| 平台 asset 缺失 | `no release available for darwin/arm64`（复用现有） |
| 降级无 --force | `v0.2.0 is older than current v0.2.16. Use --force to downgrade.` |
| 同版本 | `Already at v0.2.16, skipping.` |

## 测试

### `internal/update/update_test.go` 新增

- `TestNormalizeTag`：表驱动覆盖 `v0.2.0` / `0.2.0` / `v0.2.0-beta.1` / `abc` / `""` / `"v"` / `"1.2"`（不完整）/ `"1.2.3.4"`（多段）
- `TestGetRelease_Success`：httptest mock 返回 200 + Release
- `TestGetRelease_NotFound`：404 → 错误包含 "not found"
- `TestGetRelease_HTTPError`：500 → 错误包含状态码
- `TestCompareVersions`：表驱动覆盖
  - newer / equal / older
  - current = `DEV` → 总返回 1
  - current = `"abc"` / `""` → 总返回 1
  - current = `"v0.2.16"` 带 v 前缀 → 与不带前缀等价
  - target / current 含 pre-release（`-beta.1`）的对比

### `cmd/update_test.go` 新建

通过 `applyFunc` 注入打桩，避免真实替换二进制。结构：

```go
func setApplyFunc(t *testing.T, f func(*update.Release) error) {
    t.Helper()
    old := applyFunc
    applyFunc = f
    t.Cleanup(func() { applyFunc = old })
}

func setBuildVersion(t *testing.T, v string) {
    t.Helper()
    old := build.Version
    build.Version = v
    t.Cleanup(func() { build.Version = old })
}
```

覆盖：

- `TestRunUpdate_NoArg_AlreadyLatest`：mock GitHub latest 返回当前版本，断言 "Already up to date"，`applyFunc` 未被调用
- `TestRunUpdate_NoArg_Upgrade`：mock latest 较新，断言 applyFunc 被调用且参数 tag 正确
- `TestRunUpdate_SpecificVersion_Upgrade`：`update v0.2.20`，mock GetRelease，断言 applyFunc 被调用
- `TestRunUpdate_SpecificVersion_NormalizeWithoutV`：`update 0.2.20` 等价于 `v0.2.20`
- `TestRunUpdate_SpecificVersion_SameVersion`：target == current → "Already at" + applyFunc 未调用
- `TestRunUpdate_SpecificVersion_DowngradeRefused`：返回 error，applyFunc 未调用
- `TestRunUpdate_SpecificVersion_DowngradeWithForce`：`--force`，applyFunc 被调用
- `TestRunUpdate_InvalidSemver`：`update abc` → error，无 HTTP 调用（mock server 不被命中即可验证）
- `TestRunUpdate_TagNotFound`：mock 404，error 包含 "not found"
- `TestRunUpdate_DEVSkipsComparison`：current = "DEV"，`update v0.2.0`（即使是更旧版本）无需 --force 也能 apply

### 测试隔离

复用 `update.SetAPIBaseURLForTest` 替换 GitHub API URL。`applyFunc` 在 `cmd` 包内打桩。

## 文档同步

### `cmd/CLAUDE.md`

更新 `update.go` 行描述：
```
update.go:           update 子命令，支持 [version] 位置参数和 --force 标志；无 arg 走 CheckLatest 流程，指定版本走 GetRelease；CompareVersions 决定 upgrade/same/downgrade 分支，降级需 --force；DEV 版本跳过比较直接 apply
update_test.go:      覆盖 runUpdate 的单元测试（latest/specific/upgrade/downgrade/force/同版本/非法 semver/tag 不存在/DEV），applyFunc 钩子打桩避免真实替换二进制
```

### `internal/update/CLAUDE.md`

`update.go` 描述追加：
```
导出 NormalizeTag / GetRelease / CompareVersions 支持按 tag 查询和版本比较
```

### L3 文件头

- `cmd/update.go` `[OUTPUT]` 不变（仍 `newUpdateCmd`），`[POS]` 描述更新
- `cmd/update_test.go` 新文件，标准 L3 头
- `internal/update/update.go` `[OUTPUT]` 增加 `NormalizeTag / GetRelease / CompareVersions`

## 实现顺序（提示给后续 plan）

1. `internal/update`: 实现 `NormalizeTag` + 测试
2. `internal/update`: 实现 `GetRelease` + 测试
3. `internal/update`: 实现 `CompareVersions` + 测试（含 DEV 分支）
4. `cmd/update.go`: 引入 `applyFunc` 钩子；保持现有行为不变
5. `cmd/update.go`: 拆出 `runUpdateLatest`；引入 `[version]` arg + `--force` flag；实现 `runUpdateSpecific`
6. `cmd/update_test.go`: 新建，覆盖所有分支
7. L2/L3 文档同步
8. `make test && make vet && make lint && make build` 全绿；smoke：`./bin/makecli update --help` / `./bin/makecli update v9.9.9`（验证 404）
