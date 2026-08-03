# app info 命令设计

日期：2026-08-03
状态：已确认

## 目标

新增 `makecli app info <appKey>`：一条命令看清一个 App 的元信息与两个环境（preview / production）的部署状态，核心诉求是拿到**部署 URL**。

## 数据源

| 数据 | 接口 | 说明 |
|------|------|------|
| App 元信息 | 现有 `Client.GetApp(key)`（`MakeService.GetResource` → `/meta/v1/app`） | Key / Name / Description / Version / CreatedAt |
| 部署总览 | `POST /deployment/v1/deployment/overview`，body `{"appKey": "<key>"}`，header `X-Make-Target: MakeService.GetResource` | 双环境 status / commitSha / url / buildTaskID / deploymentID / desiredRelease / activeRelease；网关前缀 `/api/make` 由 cmd 层 `withGateway` 补齐 |

部署总览响应形态（dev 环境实测样例）：

```json
{
  "code": 200,
  "msg": "success",
  "data": {
    "tenantID": "90",
    "appKey": "apptest_001",
    "preview":    { "status": "Ready", "buildTaskID": "…", "commitSha": "9b05c7d", "url": "https://…", "deploymentID": "…", "desiredRelease": "…", "activeRelease": "…" },
    "production": { "status": "Ready", "buildTaskID": "…", "commitSha": "9b05c7d", "url": "https://…", "deploymentID": "…", "desiredRelease": "…", "activeRelease": "…" }
  }
}
```

## 架构

编排在 cmd 层顺序执行（GetApp → GetDeploymentOverview → 渲染），api 层保持「一端点一方法、只报事实」的分层边界。不做并发（两个快调用，goroutine 纯增复杂度）、不做 api 层聚合方法（编排逻辑不下沉）。

## api 层：`internal/api/deployment.go`（新文件）

按 `repository.go` / `user.go` 的「一服务一文件」惯例：

```go
// EnvDeployment 单环境部署状态
type EnvDeployment struct {
    Status         string `json:"status"`
    BuildTaskID    string `json:"buildTaskID"`
    CommitSha      string `json:"commitSha"`
    URL            string `json:"url"`
    DeploymentID   string `json:"deploymentID"`
    DesiredRelease string `json:"desiredRelease"`
    ActiveRelease  string `json:"activeRelease"`
}

// DeploymentOverview 应用双环境部署总览
type DeploymentOverview struct {
    TenantID   string         `json:"tenantID"`
    AppKey     string         `json:"appKey"`
    Preview    *EnvDeployment `json:"preview"`
    Production *EnvDeployment `json:"production"`
}

func (c *Client) GetDeploymentOverview(appKey string) (*DeploymentOverview, error)
```

设计要点：

- 环境字段用**指针**：`nil` = 该环境无部署数据。渲染层对 nil 打占位行，消除空结构体特判。
- 复用 `c.do`：Traceparent / X-Log-Id / 鉴权探针（ErrAuthFailed）自动生效。
- 错误三态（对齐 `checkGetResult` 语义）：
  - `code == 404` → `ErrNotFound`（含义 = 该 App 从未部署，**不是**命令失败）
  - 其他非 200 → 通用「API 错误 [code]: msg」上抛
  - 传输 / 解码错误 → 原样上抛

## cmd 层：`cmd/app_info.go`（新文件）

```
makecli app info <appKey> [--output table|json]
```

- 挂载：`app.go` 的 `newAppCmd()` 增加 `newAppInfoCmd()`。
- 执行序：`validateOutputFormat` → `validResourceKey`（本地先拦非法 key）→ `newClientFromProfile()` → `GetApp(key)` → `GetDeploymentOverview(key)` → 渲染。
- 错误语义：
  - `GetApp` 返回 `ErrNotFound` → 报错「App 不存在」，退出非 0。
  - `GetDeploymentOverview` 返回 `ErrNotFound` → 视为从未部署，双环境占位继续渲染，退出 0。
  - 任一接口其他错误 → 命令失败（不输出半截信息），交 `reportExecuteError` 单一出口呈现。
- 无 `--env` 过滤 flag：两行表已足够小（YAGNI）。

## 渲染

table 模式（默认）：

```
Key:         apptest_001
Name:        测试应用
Description: ...
Version:     1.0.0
Created At:  2026-01-01T10:00:00Z

┌─────────────┬────────┬─────────┬────────────────────────────────────┐
│ ENVIRONMENT │ STATUS │ COMMIT  │ URL                                │
├─────────────┼────────┼─────────┼────────────────────────────────────┤
│ preview     │ Ready  │ 9b05c7d │ https://apptest-001-preview-90.…   │
│ production  │ Ready  │ 9b05c7d │ https://apptest-001-prod-90.…      │
└─────────────┴────────┴─────────┴────────────────────────────────────┘
```

- 头部 key-value 平铺（`fmt.Printf("%-13s %s\n", …)`，cmd/CLAUDE.md 渲染约定），字段与 app list 同源：Description 取 `Properties["description"]`，Version / CreatedAt 取 `Meta`。
- 部署表用 tablewriter，固定两行 preview / production；环境为 `nil` 时该行 STATUS 列显示 `Not deployed`，COMMIT / URL 列显示 `-`。
- 表格不截断 URL（完整可复制是这条命令的核心价值）。
- JSON 模式：`writeJSON(map[string]any{"app": app, "deployment": overview})`，完整字段（含 buildTaskID / deploymentID / desiredRelease / activeRelease）；overview 为「从未部署」时 deployment 输出 `null`。

## 测试

`internal/api/deployment_test.go`（httptest 隔离网络）：

- 成功解析双环境 / 单环境缺失为 nil
- 请求体（appKey）、路径、X-Make-Target 头断言
- 404 → ErrNotFound / 非 200 业务码报错 / 传输错误 / 解码错误
- 鉴权码 990300403 → ErrAuthFailed（探针路径）

`cmd/app_info_test.go`（httptest + setProfile / captureStdout）：

- 成功：双环境表格渲染（URL 完整出现）
- 单环境 nil → 占位行
- 部署总览 404 → 双环境占位、退出 0
- App 不存在 → 报错退出非 0
- deployment 服务 500 → 命令失败
- JSON 输出（含 deployment: null 形态）
- 非法 output / 非法 appKey / 无凭证 / 未知 profile

## 风险与待验证

- **「从未部署」的实际返回形态是推测**（404 业务码或 200 + 空环境字段）。设计两种都兜住（404 → ErrNotFound → 占位；200 + 环境缺失 → nil → 占位）；若实测是其他形态（如非 404 错误码），实现时以 dev 环境实测为准回调此节。

## 文档同步

- `cmd/CLAUDE.md` / `internal/api/CLAUDE.md` 成员清单补 `app_info.go` / `deployment.go` 条目
- 根 `CLAUDE.md` cmd 行的 app 子命令清单补 `info`
