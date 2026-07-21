# makecli 发布通道（stable / beta）设计

日期：2026-07-21
状态：已与 JimYu 对齐定稿

## 目标

让 makecli 支持 beta 预发布：维护者打 `vX.Y.Z-beta.N` tag 即发 beta；用户显式切入 beta 通道后，裸 `update` 与后台更新提示自动追踪 beta；稳定用户（含所有已发行旧版本）完全无感知。

## 原理基础

GitHub Release 的 `prerelease` 布尔标志是通道归属的单一真相源：

- 存储与下载路径对稳定/beta 完全一致，标志只是查询谓词
- `GET /releases/latest` 服务端契约：返回最新的非 prerelease、非 draft release —— 稳定通道零改动的根基，对新旧二进制一视同仁
- `GET /releases` 返回全部（含 prerelease），beta 通道用此端点客户端挑 semver 最高者
- GoReleaser `release.prerelease: auto` 按 tag 的 semver 预发布段自动打标

## 设计决策（已确认）

1. **持久通道**：`[settings] channel = stable|beta`，完全复刻 `environment` 的既有模式
2. **切换入口**：`configure set channel beta` / `configure get channel`（缺省回退 stable）；update 命令不加通道 flag——临时尝鲜走既有的 `update vX.Y.Z-beta.N` 按 tag 安装
3. **beta 通道候选集含稳定版**：取全部 release 的 semver 最高者，稳定版超过 beta 时自然收敛回稳定版，无需区分分支
4. **notifier 判定用白名单**：合法 beta 版本 = prerelease 段匹配 `^beta\.\d+$`；git-describe 伪版本（`-16-ga4765c1`）与 go install 伪版本天然被拒，不引入"安装来源"标记
5. **Homebrew 不发 beta**：`brews.skip_upload: auto`，beta 用户仅走自更新
6. **不做**：/ship beta 自动化（beta tag 手动打）、alpha/rc 多级通道、Homebrew beta cask

## 分模块改动

### 1. 发布侧（.goreleaser.yml，零 CI 改动）

```yaml
release:
  prerelease: auto      # tag 带预发布段 → GitHub prerelease
brews:
  - ...
    skip_upload: auto   # 预发布版本不推 Homebrew formula
```

发版动作：`git tag v0.6.0-beta.1 && git push --tags`，现有 release workflow 对 `v*` 通配即触发。

### 2. 通道模型（internal/config）

- `channel.go`（新）：`ChannelStable = "stable"` / `ChannelBeta = "beta"` / `DefaultChannel = ChannelStable` / `ChannelNames()`，对齐 environment.go 的域常量职责
- `settings.go`：`Settings.Channel string`（空串 = 未配置），`LoadSettings` 读 `kv["channel"]`
- 写入复用既有 `SetSetting("channel", value)`，无新写路径

### 3. 更新引擎（internal/update）

`CheckLatest` 改签名（greenfield，不留兼容薄壳）：

```go
// CheckLatest(currentVersion string, includePrerelease bool)
//   false → GET /releases/latest（现状，服务端过滤）
//   true  → GET /releases?per_page=100，跳过 tag 非法 semver 的条目，取最高者（含稳定版）
```

比较与安装管线（isNewer / CompareVersions / Apply / checksums）零改动——Masterminds semver 对预发布排序天然正确，资产命名模板天然带预发布段。

### 4. update 命令（cmd/update.go）

- 新增 `resolveChannel()`：读 `LoadSettings().Channel`，空 → stable，未知值报错（对齐 resolveEnvironment 的未知名报错先例）
- `runUpdateLatest` / `runUpdateCheck` 按通道调 `CheckLatest(v, channel == beta)`；beta 通道时输出附 `(beta channel)` 回显
- 降级边界：stable 通道 + 当前是 beta 版本 + 最新稳定 < 当前 → 在 "Already up to date" 之外补一行提示：当前为 beta 版本，回稳定版可 `makecli update <tag> --force` 或等待更高稳定版
- `runUpdateSpecific`（按 tag 安装）不变——已天然支持 beta tag，降级保护复用 `--force`

### 5. notifier（internal/notifier）

- `decision.go`：`isReleaseVersion(current)` 泛化为 `versionInChannel(current, channel)`：
  - stable：prerelease 段必须为空（现状不变）
  - beta：prerelease 段为空 **或** 匹配 `^beta\.\d+$`
  - 非法 semver / DEV 恒 false；此守卫仍必须先于 CompareVersions（原因注释保留）
- `shouldNotify` 增加 channel 参数：首守卫换成 `versionInChannel`；新增短路 `cache.Channel != channel`（跨通道缓存不可用）
- `cache.go`：`cacheData` 增加 `Channel string`
- `notifier.go`：`Start` 后台 goroutine 内 `LoadSettings` 取通道（best-effort：读失败/未知值静默当 stable，notifier 不允许炸），按通道调 `CheckLatest`，落盘带 Channel；通道不匹配的缓存视为需刷新
- `Finish` 把通道传入 `shouldNotify`

### 6. version list（cmd/version_list.go）

表格加 `TYPE` 列（对齐 `gh release list`）：prerelease → `Pre-release`，否则留空。JSON 视图已含 `prerelease` 字段，不变。

### 7. 文档与样例

- `configure --sample` 模板补 `channel` 注释行（configure_test 完整性测试同步）
- configure set/get 的 Long/Example 补 channel 特殊键说明
- 各触及模块的 GEB L2 / L1 回环更新

## 边界情况清单

| 场景 | 行为 |
|---|---|
| 旧二进制 + beta 已发布 | `/releases/latest` 服务端过滤，裸 update / notifier 完全无感；`version list` 会列出（tag 自带 -beta 后缀自明） |
| beta 通道 + 更高稳定版发布 | semver 最大值自然指向稳定版，提示/升级收敛回 stable |
| 切回 stable 后 update | 最新稳定 < 手上 beta → 降级保护 + 提示文案；≥ 则正常升级 |
| 切换通道后的旧缓存 | cache.Channel 不匹配 → 不提示 + 触发刷新 |
| git-describe 伪版本 + channel=beta | `^beta\.\d+$` 白名单拒绝 → notifier 静默（开发态不变） |
| config 手改 channel=非法值 | update 命令报错列出合法值；notifier 静默当 stable |
| DEV 构建 | 现状不变：跳过比较、可任意 update、notifier 静默 |

## 测试策略

- internal/update：`CheckLatest(v, true)` 取最高（beta 最高 / 稳定反超 / 混合乱序 / 非法 tag 跳过），httptest 模拟 `/releases`
- internal/config：channel 常量与 LoadSettings 读取
- internal/notifier：`versionInChannel` 矩阵（stable/beta × 正式/beta/git-describe/DEV/非法）、cache Channel 往返、跨通道缓存短路、Start 按通道刷新
- cmd：resolveChannel 优先级与非法值、beta 通道 update/--check 走列表端点（httptest 断言请求路径）、降级提示文案、version list TYPE 列
- 发布侧：goreleaser 配置无法单测，首个 beta tag 实战验证

## 发布 runbook（首个 beta）

1. 合并本设计实现，先发一个稳定版（让存量用户拿到带通道能力的二进制）
2. `git tag v0.X.Y-beta.1 && git push --tags`
3. 验证：GitHub release 带 Pre-release 徽标；`brew` tap 无变化；旧版本 `update --check` 仍报稳定版；`configure set channel beta` 后 `update` 装上 beta
