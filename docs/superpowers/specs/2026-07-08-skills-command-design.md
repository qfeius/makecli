# makecli skills 命令组设计

日期：2026-07-08
状态：已确认（JimYu 2026-07-08）

## 目标

为 makecli 增加 `skills` 命令组，让用户不依赖 `makecli update` 的后置同步，也能独立查看、升级、删除 Make platform skills（`qfeius/make-platform-skills`）。

## 命令结构

```
makecli skills                    # 默认行为 = list（version.go 同款 gh 模式）
makecli skills list               # 本地已装 Make platform skills + 远端比对
makecli skills update             # 安装缺失 + 升级已有（复用 skillsync.Sync）
makecli skills remove <name>...   # 删除指定 skill，名字必填
```

## list：数据流与状态判定

三个数据源合并成一张表：

1. **lockfile**：`$XDG_STATE_HOME/skills/.skill-lock.json`，回退 `~/.agents/.skill-lock.json`（与 vercel-labs/skills CLI 的路径解析链一致，schema version 3）。按 `source == "qfeius/make-platform-skills"` 过滤出 Make platform skills，取 `skillFolderHash` / `installedAt` / `updatedAt`。
2. **SKILL.md frontmatter**：`~/.agents/skills/<name>/SKILL.md` 的 YAML frontmatter 取 `description`，截断为单行。
3. **GitHub Contents API**（匿名，5 秒超时）：`GET https://api.github.com/repos/qfeius/make-platform-skills/contents/skills`，一次请求拿到全部远端 skill 目录名 + tree SHA。已实测（2026-07-08，用 rorkai/app-store-connect-cli-skills 的历史 ref 验证）：lockfile 的 `skillFolderHash` 与 Contents API 返回的目录 `sha` 是同一语义（GitHub tree SHA），可直接等值比对。

状态判定：

| 本地 lockfile | 远端仓库 | STATUS |
|---|---|---|
| 有，hash 相同 | 有 | `up-to-date` |
| 有，hash 不同 | 有 | `outdated` |
| 无 | 有 | `not installed` |
| 有 | 无 | `removed upstream` |
| 有 | 网络失败 | `unknown` |

输出：

- 表格列 `NAME / STATUS / DESCRIPTION / UPDATED AT`，用 `github.com/olekukonko/tablewriter`（对齐 `app list` 渲染约定）。`not installed` 行的 DESCRIPTION 留空（避免逐个拉远端 SKILL.md 的 N 次请求）。
- 表格后汇总一行：`N installed, M outdated, K available`；当 outdated 或 not installed > 0 时追加提示 `Run 'makecli skills update' to install/upgrade.`
- `--output table|json`（默认 table），JSON 输出含 `name / status / description / installedAt / updatedAt / localHash / remoteHash`。
- 空态（lockfile 不存在或无 Make skills）：不是错误，输出引导安装的提示 + 远端可装列表（远端可达时）。

降级策略：GitHub 不可达时不失败——照常输出本地列表，STATUS 为 `unknown`，stderr 打一行警告。远端比对是增强，不是门禁。退出码恒为 0。

## update

直接调用 `skillsync.Sync`（执行 `npx -y skills add qfeius/make-platform-skills --all -y`，幂等：装缺的 + 升级已有的）。渲染复用 `cmd/update.go` 已有的 `renderSkillSyncStart` / `renderSkillSyncResult`。与 `makecli update` 后置同步是同一语义、同一代码路径，零新逻辑。`--skip-skills` 不适用于此命令（显式调用即明确意图）。

## remove

1. 读 lockfile，校验每个传入名字确属 `source == "qfeius/make-platform-skills"`——**拒绝删除其他来源的 skill**（用户机器上可能有几十个第三方 skills，makecli 不越界）。
2. 校验通过后执行 `npx -y skills remove <names> -y`（vercel-labs/skills 的 remove 已确认支持 `-y` 非交互）。
3. 名字未安装或非 Make 来源 → 报错并列出合法候选。

## 代码落位

```
internal/skillsync/
  sync.go        # 不动
  inventory.go   # 新增：lockfile 读取 + SKILL.md frontmatter 解析 + GitHub 远端比对 + 状态合并
cmd/
  skills.go          # 命令组，默认 RunE = list
  skills_list.go
  skills_update.go
  skills_remove.go
```

测试接缝沿用既有模式：

- GitHub API：包级 `apiBaseURL` 变量 + httptest（`internal/update` 同款）。
- lockfile / skills 目录路径：经参数或包级变量注入，`t.TempDir` 隔离。
- npx 执行：沿用已有的 `runSkillsCommand` seam（remove 需要同款 seam）。

## 错误处理

- lockfile 不存在 / 无 Make skills → 空态输出 + 安装引导，非错误。
- lockfile `version != 3` → stderr 警告格式版本不匹配，尽力解析。
- SKILL.md 读不到或 frontmatter 解析失败 → description 留空，不阻断。
- GitHub 请求失败/超时 → STATUS=`unknown` + stderr 警告，退出码 0。
- remove 名字校验失败 → 明确报错列出已安装的 Make platform skills。

## 明确不做（YAGNI）

- `remove --all` 一键清空。
- list 展示非 Make 来源的 skills（`--all` 之类）。
- 对 `skillFolderHash` 之外的版本概念（skills 没有 semver）。
- 项目级（project-scope）lockfile 扫描，v1 只读全局。
