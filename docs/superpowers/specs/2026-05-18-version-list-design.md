# makecli version list 命令设计

## 概述

为 `makecli version` 新增 `list` 子命令，列出 GitHub 上的历史 release。复用 `internal/update` 已有的 GitHub Releases 调用能力，输出风格与 `makecli app list` 对齐（tablewriter 边框表格 + `--output table|json` 双格式）。

## 范围

**包含：**
- `internal/update`：扩展 `Release` 结构体新增 4 个字段，新增 `ListReleases(limit int)` 函数
- `cmd/version.go`：保留 `formatVersion / changelogURL`，将 `newVersionCmd` 升级为「带默认 Run + 挂载子命令」的命令组
- `cmd/version_list.go`：新增 `newVersionListCmd` 子命令 + `runVersionList` 实现
- 测试：`cmd/version_list_test.go`、`internal/update/update_test.go` 增量
- 文档：`cmd/CLAUDE.md`、`internal/update/CLAUDE.md`、对应文件头 L3 契约

**不包含：**
- 按平台过滤可用 asset（GitHub 公开数据，未来按需）
- 离线缓存（每次实时拉取，GitHub API 公开端点无需鉴权）
- 翻页（默认 20 条够用，limit 上限 100，超过则需另写 paginated 方案）

## 命令行为

```
makecli version            # 行为不变：打印当前版本 + changelog 链接
makecli version list       # 新增：列出最近 20 条 release
```

`version` 命令同时拥有 `Run` 和子命令，Cobra 在无子命令参数时执行 `Run`，传入 `list` 时走子命令。

### Flag

| Flag | 类型 | 默认 | 含义 |
|------|------|------|------|
| `--limit` | int | `20` | 拉取条数，必须在 [1, 100] 范围内（受 GitHub `per_page` 上限约束） |
| `--output` | string | `table` | 输出格式，可选 `table` / `json` |

不需要 `--profile` / `--server-url`，与 `update` 命令同 — 走 GitHub 公开 API。

### 表格列

```
CURRENT  VERSION  PUBLISHED             NAME                   URL
*        v1.2.3   2026-05-10T08:12:00Z  v1.2.3 - patch fix     https://github.com/qfeius/makecli/releases/tag/v1.2.3
         v1.2.2   2026-05-01T03:55:11Z  v1.2.2 - perf          https://github.com/qfeius/makecli/releases/tag/v1.2.2
```

- `CURRENT`：tag 等于 `build.Version`（双方去前缀 `v`）时填 `*`，否则空
- `VERSION`：原样取 `Release.TagName`（GitHub 返回带 `v` 前缀）
- `PUBLISHED`：原样取 `Release.PublishedAt`（ISO8601），与 `app list` 的 `CREATED AT` 风格一致
- `NAME`：取 `Release.Name`，空时回退 `tag_name`
- `URL`：取 `Release.HTMLURL`

渲染用 `tablewriter.NewTable(os.Stdout)` + `Header(...) + Bulk(rows) + Render()`，遵守 `cmd/CLAUDE.md` 表格约定。

### JSON 输出

```json
[
  {
    "tag_name": "v1.2.3",
    "name": "v1.2.3 - patch fix",
    "published_at": "2026-05-10T08:12:00Z",
    "prerelease": false,
    "html_url": "https://github.com/qfeius/makecli/releases/tag/v1.2.3"
  }
]
```

不输出 `Assets` 字段（噪音；用户不需要从 `version list` 看到平台包）。

## 数据层设计（`internal/update`）

### Release 结构体扩展

```go
type Release struct {
    TagName     string  `json:"tag_name"`
    Name        string  `json:"name"`         // 新增
    PublishedAt string  `json:"published_at"` // 新增
    Prerelease  bool    `json:"prerelease"`   // 新增
    HTMLURL     string  `json:"html_url"`     // 新增
    Assets      []Asset `json:"assets"`
}
```

向后兼容：新增字段不会破坏 `CheckLatest` 与 `Apply` 现有逻辑。

### ListReleases 函数

```go
func ListReleases(limit int) ([]Release, error) {
    url := fmt.Sprintf("%s/repos/qfeius/makecli/releases?per_page=%d", apiBaseURL, limit)
    // GET → 200 → JSON decode → return
    // 非 200 → return error
}
```

- 走 `apiBaseURL`（测试可替换）
- 错误处理对称于 `CheckLatest`：HTTP 错误带状态码、解析错误带原因

## CLI 层设计（`cmd/version.go` 重构 + `cmd/version_list.go` 新增）

### version.go 改造

```go
func newVersionCmd(version, buildDate string) *cobra.Command {
    cmd := &cobra.Command{
        Use:   "version",
        Short: "Show version information",
        Run: func(cmd *cobra.Command, args []string) {
            fmt.Print(formatVersion(version, buildDate))
        },
    }
    cmd.AddCommand(newVersionListCmd())
    return cmd
}
```

`formatVersion` / `changelogURL` 保持不变。

### version_list.go 骨架

```go
func newVersionListCmd() *cobra.Command {
    var limit int
    var output string
    cmd := &cobra.Command{
        Use:   "list",
        Short: "List historical releases from GitHub",
        RunE: func(cmd *cobra.Command, args []string) error {
            return runVersionList(cmd, limit, output)
        },
    }
    cmd.Flags().IntVar(&limit, "limit", 20, "number of releases to fetch (1-100)")
    cmd.Flags().StringVar(&output, "output", "table", "output format: table|json")
    return cmd
}

func runVersionList(cmd *cobra.Command, limit int, output string) error {
    if limit < 1 || limit > 100 {
        return fmt.Errorf("limit must be between 1 and 100")
    }
    if err := validateOutputFormat(output); err != nil {
        return err
    }

    releases, err := update.ListReleases(limit)
    if err != nil {
        return err
    }

    if output == outputJSON {
        return writeJSON(toJSONView(releases))
    }
    return renderReleaseTable(releases, build.Version)
}
```

`renderReleaseTable`：构造 5 列 rows，匹配 CURRENT 标记规则后调 `tablewriter` 渲染。
`toJSONView`：将 `[]Release` 投影为去掉 Assets 的简化结构体切片。

## 错误 & 边界

| 场景 | 行为 |
|------|------|
| GitHub 网络失败 | 透传错误 |
| GitHub 返回非 200 | 返回带状态码错误 |
| `--limit < 1 \|\| > 100` | 返回 `limit must be between 1 and 100` |
| `--output` 非法值 | 复用 `validateOutputFormat`，返回错误 |
| 空列表 | table 模式打印 `No releases found.`，JSON 模式输出 `[]` |
| 当前版本是 `DEV` | CURRENT 列全空（DEV 不会匹配任何 tag） |

## 测试

### `internal/update/update_test.go` 新增

- `TestListReleases_Success`：httptest mock 返回 3 条 release，断言数量和字段
- `TestListReleases_HTTPError`：500 错误路径
- `TestListReleases_ParseError`：非法 JSON

### `cmd/version_list_test.go` 新增

- `TestRunVersionList_Table`：mock GitHub，验证 stdout 包含 VERSION/CURRENT 列与预期行
- `TestRunVersionList_TableMarksCurrent`：mock 的某 tag 等于 build.Version，验证 `*` 出现在正确行
- `TestRunVersionList_JSON`：验证 JSON 序列化正确、不含 Assets
- `TestRunVersionList_Empty`：mock 返回空数组
- `TestRunVersionList_InvalidLimit`：limit=0 和 limit=101 各一例
- `TestRunVersionList_InvalidOutput`：output=xml
- `TestRunVersionList_APIError`：GitHub 返回 500

利用 `captureStdout`（来自 `stdout_test.go`）+ 替换 `apiBaseURL`，沿用 `update_test.go` 的隔离模式。

## 文档更新

### `cmd/CLAUDE.md`

成员清单新增：
```
version_list.go:        version list 子命令，调 internal/update.ListReleases 拉取 GitHub 最近 N 条 release，tablewriter 输出 CURRENT/VERSION/PUBLISHED/NAME/URL；支持 --limit（默认20，1-100）/ --output（table|json）；CURRENT 列对比 build.Version 标记当前安装版本
version_list_test.go:   覆盖 runVersionList 的单元测试（table/JSON/空列表/非法 limit/非法 output/API 错误/CURRENT 标记），用 httptest 隔离网络
```

`version.go` 描述更新为「挂载 list 子命令 + 默认 Run 打印当前版本」。

### `internal/update/CLAUDE.md`

`update.go` 描述新增 `ListReleases` 说明，`update_test.go` 描述新增对 `ListReleases` 的覆盖。

### L3 文件头契约

- `cmd/version.go` POS 更新为「version 子命令，挂载 list 子命令，默认 Run 打印当前版本」
- `cmd/version_list.go` 新文件，添加完整 INPUT/OUTPUT/POS/PROTOCOL 头
- `internal/update/update.go` OUTPUT 添加 `ListReleases`

## 实现顺序（提示给后续 plan）

1. 扩展 `Release` 结构体 + 实现 `ListReleases`
2. `internal/update` 测试
3. 重构 `version.go` 挂子命令
4. 新增 `version_list.go`
5. `cmd` 测试
6. 文档（L1/L2/L3）同步
7. `make test && make vet && make build` 整体验证
