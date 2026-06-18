# implementation-notes — `--dry-run` for CreateResource commands

> 功能：给 `MakeService.CreateResource` 全族写命令加 `--dry-run`，注入 `X-Dry-Run: true`
> 让后端跑真实业务流程但以 ROLLBACK 替换 COMMIT（不落库）。Spec: `AgenticDSL/Design/DryRun.md`。

## 落地结构（一次机制，四处挂旗）

- **`internal/api`**：新增 `WithDryRun(on bool)` 选项 + `Client.dryRun` 字段。`do()` 在唯一请求
  咽喉点注入 `X-Dry-Run: true`（与 Traceparent/X-Log-Id 并列），CreateResource 全族自动继承；
  `--debug` 的 curl 输出同步打印该头。
- **`cmd/client.go`**：`newClientFromProfile(...api.Option)` 收变参——基础选项
  （`WithDebug`/`WithHeaders`）后追加每命令横切选项。profile/server/env 仍走全局不穿参。
- **`--dry-run` 旗**：`app create` / `entity create` / `relation create` / `record create`。

## 规范外的实现选择 / 取舍

1. **`app create --dry-run` 跳过一切本地副作用。** 远端校验通过后立即收尾——不写脚手架、不 `git init`、
   不 commit、不准备代码仓库。dry-run 只回答「远端创建会不会成功」；写本地文件、建仓库都是真实副作用。
   脚手架模式与 `-f` 文件模式同款短路。
2. **输出 would-be 语义，不冒充真实创建。** 统一打印
   `Dry run: <kind> '<key>' would be created successfully (no changes made)` 即返回；API 失败
   （如 key 冲突）按普通错误透传——CLI 从 `code` 判定，契合 spec「无需从响应里区分」。
3. **`record create --dry-run` 不展示 recordID。** 事务回滚后返回的 recordID 不指向真实记录，展示会误导。
4. **范围 = 全部 create 命令（JimYu 定稿）。** `apply` 暂不挂：含 UpdateResource 路径，其 dry-run
   后端本期未覆盖。

## GEB 文档回环

- L3：`internal/api/client.go`、`cmd/client.go`、四个 `*_create.go` 头部 INPUT/OUTPUT/POS 已更新；
  `client_test.go` / `app_create_test.go` / `entity_create_test.go` 头部同步。
- L2：`internal/api/CLAUDE.md`（WithDryRun + do() 注入说明）、`cmd/CLAUDE.md`
  （newClientFromProfile 变参约定 + 四个 create 条目）。
- L1：根 `CLAUDE.md` api 条目补注 X-Dry-Run。

## 验证

`make vet` ✅ / `make test` ✅ / `golangci-lint run ./...` ✅ 0 issues。
新增测试：`TestWithDryRun`（开启注头 / 默认及 false 均缺席）、app-create dry-run（头到达线缆 +
零本地文件 + would-be 输出 + 失败透传）、entity-create dry-run（无副作用路径代表）。
