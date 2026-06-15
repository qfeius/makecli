# makecli login — 设计 spec

> 把 `contract-cli auth login --as user` 的浏览器 OAuth 登陆流程移植到 `makecli login`。
> 最小移植：只做 user 浏览器流，拿到 access_token 写进 `~/.make/credentials`。

---

## 1. 目标与非目标

**目标**
- 新增 `makecli login`：打开浏览器让用户登陆 → 拿到 access_token → 写入 `~/.make/credentials[<profile>]`。
- 写入后 `resolveProfile()` / `newClientFromProfile()` **零改动**即可消费该 token。

**非目标（YAGNI，本期不做）**
- bot 身份（tenant_access_token）。
- refresh_token / expiry 的持久化与自动刷新。
- 多环境（prod）切换——仅硬编码 dev preset。
- `logout` / 登陆状态查询子命令（如需后续补，成本极低）。

---

## 2. 命令面

```
makecli login [--timeout 5m] [--no-open-browser]
```

| 标志 | 来源 | 默认 | 说明 |
|---|---|---|---|
| `--profile` | **root 全局 PersistentFlag**（复用，禁止本地声明） | `default` | token 写进哪个 `[profile]` |
| `--timeout` | 本地 | `3m` | 等回调超时；你的 `--timeout 5m` 照常工作 |
| `--no-open-browser` | 本地 | `false` | 只打印授权 URL，不自动开浏览器 |

`login` 为新顶级命令（非 `auth` 组），在 `cmd/root.go` 注册。身份固定为 user，无 `--as` 旋钮。

---

## 3. 登陆流程（一条直线，无持久化分支）

```
1. discover(metadataURL)         → authorization_endpoint / token_endpoint / registration_endpoint
2. 起回调 server(127.0.0.1:0)    → OS 分配空闲端口，得到 redirectURL = http://127.0.0.1:<port>/callback
3. register(fresh client)        → client_id（每次新注册，redirect_uris=[redirectURL]）
4. PKCE                          → code_verifier + S256 challenge + state
5. buildAuthURL → 开浏览器       → (--no-open-browser 时改为打印 URL)
6. 等 callback(code, timeout)    → 校验 state，取 code
7. exchange(code)                → access_token (+ 内存里的 expiry 仅用于提示)
8. 写 ~/.make/credentials        → [<profile>] access_token = <JWT>
9. 打印成功（含过期时间，如有）
```

### 两个设计判断（已与 JimYu 对齐）

**判断 A — 每次登陆都重新注册 client，不持久化 client_id。**
contract-cli 持久化 client_id 是为了"首次才注册"的 `if clientID == "" {…}` 分支。本期选"只写 access_token"，再为 client_id 单开 config key + 读写 + 校验得不偿失。**每次注册 = 删掉该分支**，控制流更干净。代价：每次 login 在授权服务器留一条 client 记录（public PKCE client，可接受）。

**判断 B — 回调端口动态分配（`127.0.0.1:0`）。**
bind 空闲端口 → 读实际端口拼 redirect_uri → 注册它 → 用它。与判断 A 天然咬合：每次现注册，端口变了永不失配；同时彻底删掉固定端口 8000 这个特殊值，连 `--port` 旋钮都不需要。

> A 与 B 互斥锁定：动态端口要求"每次注册"（注册一次的 redirect_uri 端口下次对不上），故二者绑定成立。

---

## 4. make preset + 按 profile 的 auth-server-url

```go
const (
    authBusinessType   = "make"
    authResource       = ""                              // 留空：授权/换 token 不带 resource 参数
    authClientName     = "makecli"
    defaultAuthBaseURL = "https://dev-myaccount.qtech.cn" // auth-server-url 未配置时的回退
)
var authScopes = []string{"make:resources"}
```
位置：`cmd/login.go`，与 `root.go` 的 `defaultMetaServer` 等常量风格一致。

**身份服务器按环境隔离（事后修订）**：OAuth 元数据地址不再写死 dev，而是由 profile 的
`auth-server-url`（`~/.make/config`，与 `server-url` 同级）派生：
`authMetadataURL(authBase) = TrimRight(firstNonEmpty(authBase, defaultAuthBaseURL),"/") + "/.well-known/oauth-authorization-server/make"`。
否则 `--profile test` 会用 dev 身份服务器颁发 token，再打到 test 后端（`test-make.qtech.cn`）
被拒（`token验证失败`）。dev 留空回退；test 设 `auth-server-url = https://test-myaccount.qtech.cn`。

---

## 5. 落盘

- **只**写 `~/.make/credentials` 的 `[<profile>] access_token = <JWT>`，复用现成 `config.Load()` / `config.Save()`。
- refresh_token / expiry 仅在内存用于成功提示（"Access token expires at: …"），**不落盘**。
- `~/.make/config` 不动（不新增任何 key）。

---

## 6. 代码结构

### 新建 `internal/oauth/`（纯协议原语，无 cobra，可单测）

| 文件 | 职责 |
|---|---|
| `pkce.go` | `NewCodeVerifier(reader)` / `NewState(reader)` / `S256Challenge(verifier)` |
| `discovery.go` | `DiscoverFromAuthorizationServer(ctx, client, metadataURL)` → 端点三元组；`AuthorizationServerMetadata` 类型 |
| `registration.go` | `RegisterClient(ctx, client, endpoint, req)` → `client_id`（RFC 7591 动态注册） |
| `flow.go` | `BuildAuthorizationURL(req)` / `ExchangeAuthorizationCode(ctx, client, req)` → `Token`；`StartCallbackServer()`（bind `127.0.0.1:0`，返回 server + 实际 redirectURL）/ `(*CallbackServer).Wait(ctx, state)` / `Close()`；`OpenBrowser(url)`；本地最小 `Token{AccessToken, TokenType, Scope, RefreshToken, Expiry}` 类型 |

从 contract-cli 移植，**砍掉** bot 的 `ExchangeTenantAccessToken` 及 `Discover`（protected-resource 两跳发现，本期用单跳 `DiscoverFromAuthorizationServer` 即可，因为直接给了 authorization-server metadata URL）、`slog.Logger` 依赖（makecli 这层不需要结构化日志，错误经 `error` 上抛）。

`StartCallbackServer` 签名相对 contract-cli 调整：**不再接收 redirectURL 入参**，改为内部 bind `127.0.0.1:0` 并返回 `(*CallbackServer, redirectURL string, error)`——这是判断 B 的落点。

### 新建 `cmd/login.go`（编排 + preset + 命令注册）

- `newLoginCmd()` → 顶级 `login` 命令。
- `runLogin(timeout, noOpenBrowser)`：编排 §3 流程，调 `internal/oauth` 原语，最后 `config.Save` 写 token。
- 持 §4 preset 常量。
- 测试桩：包级 `var openBrowserFunc = oauth.OpenBrowser`，单测覆盖为"直接 GET 回调 URL 注入合法 code"，免真浏览器。

### 修改 `cmd/root.go`

- `rootCmd.AddCommand(newLoginCmd())`。

---

## 7. 错误处理

- discovery / registration / exchange 任一步 HTTP 非 2xx：读 body（限长）拼进 error 上抛，`SilenceUsage: true` 不污染 usage。
- 回调超时 / state 不匹配 / 缺 code：明确 error。
- discovery 回来缺 `registration_endpoint`（见 §9 风险）：在注册步骤报 `"authorization server does not advertise registration_endpoint; a fixed client_id is required"`，提示需回退固定 client_id 方案。
- 浏览器打开失败：`Warn` 级别——不致命，继续等回调（用户可手动开打印出的 URL）。

---

## 8. 测试策略

| 层 | 覆盖 |
|---|---|
| `internal/oauth/pkce_test.go` | 种子化 reader → verifier/state 确定性；S256 已知向量 |
| `internal/oauth/discovery_test.go` | httptest 返回 metadata JSON → 解析端点；非 2xx / 坏 JSON 错误路径 |
| `internal/oauth/registration_test.go` | httptest → client_id；缺 client_id / 非 2xx 错误路径 |
| `internal/oauth/flow_test.go` | `BuildAuthorizationURL` 纯函数断言 query；`ExchangeAuthorizationCode` httptest；回调 server bind→拿端口→GET 注入 code→`Wait` 取回；state 不匹配 / 超时 |
| `cmd/login_test.go` | httptest 同时 mock metadata/registration/token 三端点；`openBrowserFunc` 桩里 GET 回调 URL 注入 code；断言 token 落进 `MAKE_CLI_CONFIG_DIR` 隔离的 credentials；`--no-open-browser` 打印 URL；超时路径 |

`make vet && make test` + `golangci-lint run ./...` 全绿才提交（CLAUDE.md 开发纪律）。

---

## 9. 假设与风险

- **R1（中）**：make 授权服务器须暴露 `registration_endpoint`。与 contract-cli 同服务器（`dev-myaccount.qtech.cn`），大概率有。否则回退"JimYu 提供固定 client_id"，删掉注册步骤。
- **R2（低）**：服务器须接受任意 loopback 端口做 redirect_uri（RFC 8252 native-app 惯例）。若拒，回退固定端口 `127.0.0.1:8000` 并注册之。
- **R3（低）**：每次 login 产生一条 client 注册记录（判断 A 代价）。若服务器侧介意，再引入 client_id 持久化。

---

## 10. GEB 文档同步（落地时一并完成）

- 新增 `internal/oauth/CLAUDE.md`（L2，成员清单）。
- 各新文件加 L3 头（INPUT/OUTPUT/POS/PROTOCOL）。
- 更新 `cmd/CLAUDE.md`：成员清单加 `login.go`；root 全局标志段无需改。
- 更新根 `CLAUDE.md`：`<directory>` 加 `internal/oauth/`；cmd 子命令清单加 `login`。

---

## 11. 后续（非本期）

- `logout` / 登陆状态查询。
- prod preset + 环境切换。
- refresh_token 持久化 + token 过期自动刷新。
- 动态空闲端口已在本期落地（判断 B）。
