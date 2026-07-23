# internal/agent/
> L2 | 父级: /CLAUDE.md

keyless 本地 agent（隐藏命令 `makecli agent` 的执行层，agent-design/Design.md §8.2）：标准 OpenAI 兼容客户端指向 gateway `/v1/chat/completions`，模型名用平台别名，配额与五维计量在模型面自动生效——设备端零厂商 key。默认即 code agent：gateway Provider（llm/gateway.go）+ 七工具注册表（root=cwd）+ 目录信任确认钩子 + 两层循环（code.go 编排、render.go 行式渲染）；`--no-tools` 退回 v1 纯聊天（client.go/repl.go 保留不动）。内核五子包自 github.com/smallnest/pigo（MIT）移植。

## 子包
- `core/`: agent 内核叶子类型（Content/Message/AgentEvent 密封接口族、EventStream 泛型流、AgentTool 契约、hooks），纯 stdlib，其余子包的公共地基
- `tool/`: 内建工具（read/write/edit/grep/find/ls/bash）+ 注册表（JSON Schema 校验）+ 三段式/批量执行器；失败一律编码为 IsError 工具结果
- `llm/`: LLM 流式抽象纯类型（StreamFn/LlmContext/StreamConfig/事件集/Provider）+ GatewayProvider（OpenAI 兼容 SSE 指向 gateway 的唯一具体 Provider）
- `loop/`: 两层 agent 循环（流式响应 → 工具批执行 → 回喂；六钩子）+ 系统提示词组装（AGENTS.md 目录链）；compaction/subagent 已裁剪
- `trust/`: 按目录的信任决策持久化（~/.make/agent-trust.json），副作用工具授权依据

## 成员清单
- `client.go`: Client（ChatStream：SSE 流式补全，增量回调 + 全文返回）、Message、NewSessionID（X-Session-ID 计量维度，无安全语义）、APIError（OpenAI 风格错误体还原，解不出保底状态码+原文摘要）
- `client_test.go`: 覆盖 ChatStream 的单元测试（SSE 增量解析与 headers / OpenAI 错误体还原 / session id 随机性），httptest 隔离网络
- `repl.go`: RunOnce（单条 prompt 流式打印）与 RunREPL（读一行发一轮，历史进程内存续；/exit /quit 退出、/clear 清空历史；单轮失败回滚本轮 user 消息不退出循环）——纯聊天路径（--no-tools）
- `repl_test.go`: 覆盖会话编排的单元测试（RunOnce 流式输出与 system 携带 / REPL 多轮历史携带 / /clear 清空），假 gateway 回显 messages
- `code.go`: CodeOptions、RunCodeOnce（-p 无头：一轮循环到自然收尾，终结失败返 error 转退出码 1；未信任目录副作用工具直接拦截并提示 --approve）、RunCodeREPL（读一行发一轮共用历史；/exit /quit /clear 语义同纯聊天；确认与主循环共用同一 bufio.Reader）、newCodeSession（BuildSystemPrompt[root=cwd AGENTS.md 链] + 七工具注册[root=cwd] + trust.Manager[坏存储降级会话信任] + GatewayProvider 接线；EventBuffer 必须保持 0——确认钩子在 producer goroutine 读写终端）、trustBeforeToolCall（bash/write/edit 门控：受信放行；y=一次 / n=拒绝 / a=会话信任；拦截语作 IsError 工具结果回喂）、confirmToolCall
- `render.go`: runRenderer（行式渲染：assistant 文本按 partial 水位差增量直写、⚙ 工具行、结果截断预览[首 3 行/200 字符，IsError 红 ✗ 前缀]、turn 边界空行、TurnEnd 采集终结失败进 runErr 不打印）、toolCallSummary（bash→command/文件工具→path/搜索→pattern，确认提示复用）、previewLines/truncateOneLine
- `code_test.go`: 覆盖 code agent 编排（无头文本轮含 system/AGENTS.md/7 工具声明线缆、read 工具回喂轮、未信任 bash 拦截 + --approve 放行、REPL 确认 y/n/a 三态与提示次数、/clear 清史与历史接续、网关 401 转终结失败），脚本化 SSE 假 gateway + MAKE_CLI_CONFIG_DIR 隔离信任存储

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
