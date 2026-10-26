# AI 变更记录：构建日志查询

## 2026-10-26：all 零历史跟随保留新增部署事件（TC-013）
- 摘要：修复 all + tail=0 + follow 在阶段切换时再次跳过历史，遗漏命令跟随后新增的部署事件及快速失败原因。
- 涉及模块：`cmd/logs_stages.go`、新增 `logs_follow_start_test.go`、命令帮助、README 与 cmd 模块地图。
- 决策：在跟随构建前逐页建立部署游标，首次空页即固定起点；初始化不查询任务或补拉终态，后续正式读取保留终态补拉及构建→部署输出顺序。
- 兼容：只有无续读游标的 all + tail=0 + follow 提前初始化；快照、其他 tail 值、单阶段和 cursor 续读沿用原有语义。
- 错误：初始化存储/游标错误直接报错，继续使用同一 context 和超时，保留 124/130 分类。
- TDD：先复现 stdout 仅有 B_NEW 而游标已越过部署失败，再加入初始化与历史分页、完成任务、错误及取消回归。
- 验证：全量竞态、vet/build、npm 5 项、lint 及独立复审通过；新增三函数覆盖率 100%；真实 Test/Beta 任务 505 修复版与全量输出逐字节一致，原版遗漏 875 字节。

## 2026-10-19：build 查询统一传 prod
- 摘要：logs deploy 按 appRole 原样发送 build 查询的 environment（beta/prod）。
- 涉及模块：`cmd/logs_deploy.go`、`cmd/logs_app_role_test.go`、根与 cmd 模块地图。
- 决策：应用配对仍沿用共享环境词汇；兼容任务响应中的 production，JSON appRole 不变。
- TDD：先将六个 Prod 阶段/跟随场景及生产配对请求断言改为 prod，复现 production 请求导致的失败后修复。
- 验证：请求契约、全量测试、竞态、lint、编译及 18 个 CLI 场景通过；dev 三阶段 Beta 可读，Prod 无任务明确报错。

## 2026-10-19：统一日志 JSON 角色字段为 appRole
- 摘要：build/deploy/all 的 JSON 快照使用 appRole（beta/prod），移除旧 env 字段，与 --appRole 参数对应。
- 涉及模块：`cmd/logs_deploy.go`、`cmd/logs_stages.go`、`cmd/logs_app_role_test.go`、README 和命令地图。
- 决策：只调整日志呈现视图；构建任务查询的 environment 参数、角色映射和其他命令的 JSON 不变。
- TDD：先将九个角色/阶段快照契约改为要求 appRole 且拒绝 env，复现原输出失败后再调整两个输出路径。
- 验证：字段契约、全量测试、竞态、lint 和编译通过；编译后 CLI 18 个场景、dev 接口 6 个快照均通过。

## 2026-10-19：统一日志角色参数为 appRole
- 摘要：日志命令使用 `--appRole beta|prod`（默认 beta），移除该命令的 --env；同步帮助、校验错误和超时重试提示。
- 涉及模块：`cmd/logs_deploy.go`、`cmd/logs_stages.go`、`cmd/logs_app_role_test.go`、README 和模块地图。
- 决策：内部选项改名 appRole；接口 environment、prod→production 映射、JSON env 及任务配对校验保持原语义。
- TDD：先将角色测试切换到 --appRole，复现 unknown flag；增加默认值及旧 --env 被拒绝的契约测试，再调整实现。
- 验证：make vet/test/build、竞态检查、lint 0 issues；编译后 12 组角色/阶段/输出组合及 dev Beta 可读、Prod 无任务不回退检查通过。

## 2026-10-19：增加 beta/prod 日志环境选择
- 摘要：`logs deploy --env beta|prod` 默认 beta；三个日志阶段共用环境选择，JSON 增加 env。
- 涉及模块：`cmd/logs_deploy.go`、`cmd/logs_stages.go`、`cmd/logs_env_test.go`、README 及命令地图。
- 决策：prod 映射 API production，按 SHA 和环境选定任务后固定 ID；应用配对校验改为匹配所选环境。
- 边界：拒绝不匹配的任务环境及跨任务组合游标；生产任务缺环境身份时报错；等待和超时重试提示保留 env。
- TDD：新增测试先复现缺少 --env 和 JSON env，再实现环境选择；覆盖默认值、build/deploy/all 快照与跟随、配对、非法输入及超时。
- 验证：make vet/test/build、竞态检查、lint 0 issues 及编译后 12 组环境/阶段/输出组合通过；dev 同 SHA 的 Beta 可读、Prod 无任务且未回退 Beta；新增环境辅助逻辑覆盖率 100%。

## 2026-10-19：修复部署终态事件漏读
- 摘要：任务终态不再直接替代事件 completed；观察终态后从当前事件游标再次读取，继续分页至 completed 或后续空页。
- 涉及模块：`cmd/logs_stages.go`、`cmd/logs_terminal_test.go`、命令模块文档。
- 决策：终态已在选任务时确认的，后续首次事件查询即满足补拉条件；运行中任务按固定 ID 查终态后继续读取，不重置游标或总超时。
- 边界：无终态事件时补拉到空页可结束；补拉失败按错误返回，等待仍受同一 context 控制；首次 tail 与后续增量读取语义保持不变。
- TDD：先复现空事件响应后任务变终态，12 个 deploy/all 快照及 follow 场景均丢失最后两页，再用终态观察标记修复。
- 专项：新增 18 个场景，覆盖 SUCCESS/FAILED/CANCELED、0/1 行 tail 不重复应用、空页、错误、异常游标及超时。
- 验证：make vet/test/build、golangci-lint 0 issues、日志与 API 竞态检查通过；review 原始漏读复现测试已转为通过。

## 2026-10-19：修复 stderr 阻塞时无法按超时退出
- 摘要：日志读取、debug 转储和最终错误打印统一由现有进程 worker 负责，取消后不等待阻塞写入。
- 涉及模块：`cmd/logs_deploy.go`、`cmd/errors.go`、对应测试与命令文档。
- 决策：`commandOutputError` 标记输出归属，根错误出口跳过重复打印；错误链保留，退出码及正常鉴权指引不变。
- 边界：stderr 堵塞时允许最后一条提示丢失，以保证 124/130 退出；不增加每条日志的 goroutine。
- TDD：先以完整根命令子进程复现 400ms 超时被阻塞至 3s 强杀，再补标记类型及 worker 内最终错误呈现。
- 专项：三个阶段的 stderr 阻塞 9 场景、stdout 阻塞 6 场景及正常错误仅打印一次的测试通过。
- 验证：make vet/test/build、golangci-lint 0 issues、日志及公共错误处理竞态测试通过；原二进制复现场景现可在超时后返回 124。

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
