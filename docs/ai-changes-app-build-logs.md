# AI 变更记录：构建日志查询

## 2026-10-19：补齐 deploy / all 阶段
- 摘要：`logs deploy --stage` 支持 build（默认）、deploy、all；移除仅限 build 的参数限制。
- 涉及模块：`cmd/logs_deploy.go`、`cmd/logs_stages*`、`cmd/logs_pipe_test.go`、`internal/api/build.go`、`internal/api/deployment_log*` 及分层文档。
- 决策：部署日志接入现有 deployment/events；CLI 按页读取后保留最后 N 行，首次截取后按事件序号续读，无服务端变更。
- all：按构建、部署顺序输出，每阶段独立 tail；组合游标保存两个阶段位置并绑定任务，共用一次 follow 总超时。
- 边界：构建失败且未部署时结束读取；无终态事件时固定任务 ID 查状态；先输出构建内容，再等待部署请求。
- TDD：先复现阶段拒绝/缺客户端方法，再复现慢部署吞掉构建输出、空日志组合游标不可续读及路由字段影响状态 JSON，逐项修复。
- 验证：make vet/test/build、golangci-lint 0 issues、日志相关竞态测试通过；dev 任务 1837 的 23 组检查通过，all 内容等于 18,951 字节构建日志 + 1,438 字节部署事件。

## 2026-10-16：日志命令与 tail/timeout
- 摘要：改为 `logs deploy`，按 app.yaml/--app 与 HEAD/--commit-sha 查询；移除公开任务 ID 参数。
- 涉及模块：`cmd/logs_deploy*`、`cmd/logs_pipe_test.go`、命令注册、`internal/api/build_log*` 与 GetAppContext。
- 决策：初次服务端 tail 默认 200，0 跳过历史，-1 全量；后续固定任务和字节游标，tail 与 cursor 互斥。
- 超时：follow 默认 3m；单个进程输出 worker 使阻塞 stdout 仍可按 124/130 退出，不给每个日志块创建 goroutine。
- 依赖：make-build-service 同步增加 tailLines 与回执；旧服务明确报错，可显式 --tail=-1。首版仅支持 build 阶段。
- TDD：先验证新入口/参数/API 缺失的红测，再补实现；另以空 cursor、tail 范围异常红测修正边界。
- 验证：make vet/test、lint 0 issues、相关竞态测试通过；本地 CLI→Build HTTP→TOS SDK 联调覆盖 tail/游标/0/-1/follow/124。

## 2026-10-16：升级前补充验证完成
- 真构建：App 提交 `9f4138d` 触发任务 1821，构建及部署 SUCCESS，完整日志 18,940 字节。
- 真接口：HEAD / SHA / ID 定位、实时跟随、游标续读与完整内容拼接一致，完成后续读为空。
- 真实 CLI：补测 16 个场景全部通过，含仓库外查询、JSON/正文、无效凭证、任务不存在、超时 124、中断 130 和参数校验。
- 自动化：日志专项 12 组测试及子项、竞态检查通过；覆盖失败任务、空日志、鉴权/存储错误、异常游标与输出中断。
- 回归：`make vet`、`make test`（Go 全量及 npm 5 项）通过；golangci-lint 2.12.2 检查 0 issues。
- 边界：真接口实测为 dev 后端及 macOS arm64；失败任务/存储异常以模拟接口覆盖，`--follow --output json` 按设计拒绝。
- 已知独立问题：preflight G1 对 `_beta_` 应用名误报，不影响本轮日志读取及实际构建。

## 2026-10-16：本地检查与真实构建验证
- 检查：`make vet`、全量 Go 与 npm 测试通过；Go 1.25.8 下 golangci-lint v2.5.0 检查 0 issues。
- 竞态：新增命令与 API 的 `go test -race` 通过；真实超时返回 124，SIGINT 返回 130。
- 真接口：本地 CLI 按 HEAD、完整 SHA、ID 均读取到任务 1814 的 18,980 字节完整日志。
- 新构建：App 文档提交 `18f8187` 已推送并触发任务 1820，CLI 正确定位此次 HEAD。
- 外部阻塞：构建 Pod `FailedScheduling`，可选节点 CPU / ENI IP 不足及节点池污点限制；全流程尚未完成。
- 本地安装：`~/.local/bin/makecli`，版本 `0.5.19+app-logs`；未发布正式版本。

## 2026-10-16：实现 app logs
- 变更摘要：新增只读构建日志命令，支持 HEAD、完整 SHA、任务 ID、游标快照与跟随。
- 涉及模块：`cmd/app_logs*`、`internal/api/build_log*`，以及共享 HTTP、JSON 输出、命令注册和退出码。
- 关键决策：HEAD + beta 只解析一次任务 ID；固定 ID 续读；认证与 Trace 共用；请求和等待支持取消。
- 兼容性：原请求入口委托可取消实现；原 JSON 出口保留；follow 默认正文，JSON 仅快照。
- TDD：先验证缺少命令/API 的失败，再复现错误游标重复输出和快照超时误分类，修复后新增测试通过。
- 回归修复：已有“升级检查已是最新”测试读取本机 beta 通道而失败；为该测试补充临时配置隔离。
- 验证：本地检查通过；真实 App 构建的集群资源阻塞见上方记录。
