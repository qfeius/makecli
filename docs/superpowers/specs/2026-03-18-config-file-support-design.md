# Config File Support (~/.make/config)

## Summary

为 makecli 增加 `~/.make/config` 配置文件支持，存储 `x-tenant-id` 和 `operator-id`，按 profile 隔离，请求时自动注入为 HTTP Header。同时重构 `configure` 命令为命令组，增加 `token` / `config` / `set` / `get` 子命令。

## 文件格式

`~/.make/config`，INI 格式，与 `~/.make/credentials` 对称：

```ini
[default]
x-tenant-id = tenant_abc
operator-id = op_123

[staging]
x-tenant-id = tenant_staging
operator-id = op_456
```

- 所有字段可选，缺失不报错
- 权限 0600，目录 0700（与 credentials 一致）

## 命令体系

```
makecli configure                          → 默认 = configure token（向后兼容）
makecli configure token   [--profile xxx]  → 交互式配置 access_token
makecli configure config  [--profile xxx]  → 交互式配置 x-tenant-id / operator-id
makecli configure set <key> <value> [--profile xxx]  → 单条写入 config
makecli configure get <key> [--profile xxx]          → 单条读取 config
```

### configure token

行为与现有 `configure` 完全一致：提示输入 access_token，JWT 校验，写入 `~/.make/credentials`。

### configure config

交互式依次提示：
1. `x-tenant-id` — 显示当前值（遮掩），回车保留
2. `operator-id` — 同上

写入 `~/.make/config`。

### configure set / get

- `set` / `get` 仅操作 `~/.make/config`；token 管理请用 `configure token`
- `set` 合法 key：`x-tenant-id`、`operator-id`
- `get` 输出对应 profile 下的值，不存在则输出空行
- 非法 key 报错：`unknown config key 'xxx', valid keys: x-tenant-id, operator-id`

## 代码变更

### 1. `internal/config/config.go`（新文件）

```go
type ConfigProfile struct {
    XTenantID  string
    OperatorID string
}

type Config map[string]ConfigProfile

func ConfigPath() (string, error)
func LoadConfig() (Config, error)
func SaveConfig(cfg Config) error
```

- INI 解析复用与 credentials 相同的模式
- key 映射：`x-tenant-id` → `XTenantID`，`operator-id` → `OperatorID`

### 2. `internal/api/client.go`（修改）

Client 增加 `headers map[string]string`：

```go
type Client struct {
    baseURL    string
    token      string
    headers    map[string]string  // 新增：自定义 header
    httpClient *http.Client
    debug      bool
}
```

New 函数签名变更为 functional options：

```go
type Option func(*Client)

func WithDebug(on bool) Option
func WithHeaders(h map[string]string) Option
func New(baseURL, token string, opts ...Option) *Client
```

`do()` 方法在设置固定 header 后，遍历 `headers` 追加：

```go
for k, v := range c.headers {
    req.Header.Set(k, v)
}
```

debug 模式同步输出这些 header 到 curl 命令，格式为额外的 `-H 'key: value'` 行。

### 3. `cmd/configure.go`（重构）

从单命令重构为命令组：

```
configure (RunE = runConfigureToken，裸调兼容)
├── token   (RunE = runConfigureToken)
├── config  (RunE = runConfigureConfig)
├── set     (RunE = runConfigureSet, Args = ExactArgs(2))
└── get     (RunE = runConfigureGet, Args = ExactArgs(1))
```

- `--profile` 设为 `configure` 的 **PersistentFlag**，所有子命令自动继承
- `prompt` / `mask` / `validateJWT` 保留在同一文件
- config 交互和 set/get 逻辑可放同文件或拆分（视行数决定）

### 4. `cmd/client.go`（新文件）— 提取公共 helper

8 个命令文件重复「加载 creds + config → 校验 profile → 构建 client」逻辑，**必须**提取：

```go
// newClientFromProfile 从 credentials + config 构建 API 客户端
func newClientFromProfile(profile, server string) (*api.Client, error)
```

内部逻辑：
1. `config.Load()` → 取 `profile` 的 `AccessToken`，缺失报错
2. `config.LoadConfig()` → 取 `profile` 的 `XTenantID` / `OperatorID`，缺失静默跳过
3. 构建 `headers map[string]string`
4. 返回 `api.New(server, token, api.WithDebug(DebugMode), api.WithHeaders(headers))`

### 5. 各命令 `runXxx` 函数（批量迁移）

所有 `api.New(server, p.AccessToken, DebugMode)` 调用替换为 `newClientFromProfile(profile, server)`。

**迁移前：**
```go
creds, err := config.Load()
p, ok := creds[profile]
client := api.New(server, p.AccessToken, DebugMode)
```

**迁移后：**
```go
client, err := newClientFromProfile(profile, server)
```

受影响文件（8 个）：
- `cmd/app_create.go`
- `cmd/app_list.go`
- `cmd/app_delete.go`
- `cmd/entity_create.go`
- `cmd/entity_list.go`
- `cmd/entity_delete.go`
- `cmd/apply.go`
- `cmd/diff.go`

## 不变的

- `~/.make/credentials` 格式和读写逻辑不动
- `--profile` / `--server` / `--debug` flag 行为不变
- 现有测试全部保持通过

## 测试策略

- `internal/config/config_test.go` — 覆盖 LoadConfig / SaveConfig / ConfigPath，与 credentials_test 对称
- `cmd/configure_test.go` — 增加 set/get/config 子命令测试，mask/validateJWT 测试不变
- `internal/api/client_test.go` — 验证 WithHeaders option 正确注入 header
- 各命令测试 — 验证 config 存在时 header 被发送
