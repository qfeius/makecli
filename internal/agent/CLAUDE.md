# internal/agent/
> L2 | 父级: /CLAUDE.md

keyless 本地 agent（隐藏命令 `makecli agent` 的执行层，agent-design/Design.md §8.2）：标准 OpenAI 兼容客户端指向 gateway `/v1/chat/completions`，模型名用平台别名，配额与五维计量在模型面自动生效——设备端零厂商 key。v1 纯聊天（流式 REPL + 一次性模式）已通；code agent 内核（core/tool/llm/loop/trust 五子包）自 github.com/smallnest/pigo（MIT）移植就位，与聊天通道的接线随后续 goal 进入。

## 子包
- `core/`: agent 内核叶子类型（Content/Message/AgentEvent 密封接口族、EventStream 泛型流、AgentTool 契约、hooks），纯 stdlib，其余子包的公共地基
- `tool/`: 内建工具（read/write/edit/grep/find/ls/bash）+ 注册表（JSON Schema 校验）+ 三段式/批量执行器；失败一律编码为 IsError 工具结果
- `llm/`: LLM 流式抽象纯类型（StreamFn/LlmContext/StreamConfig/事件集/Provider），gateway 适配器后续在此落地
- `loop/`: 两层 agent 循环（流式响应 → 工具批执行 → 回喂；六钩子）+ 系统提示词组装（AGENTS.md 目录链）；compaction/subagent 已裁剪
- `trust/`: 按目录的信任决策持久化（~/.make/agent-trust.json），副作用工具授权依据

## 成员清单
- `client.go`: Client（ChatStream：SSE 流式补全，增量回调 + 全文返回）、Message、NewSessionID（X-Session-ID 计量维度，无安全语义）、APIError（OpenAI 风格错误体还原，解不出保底状态码+原文摘要）
- `client_test.go`: 覆盖 ChatStream 的单元测试（SSE 增量解析与 headers / OpenAI 错误体还原 / session id 随机性），httptest 隔离网络
- `repl.go`: RunOnce（单条 prompt 流式打印）与 RunREPL（读一行发一轮，历史进程内存续；/exit /quit 退出、/clear 清空历史；单轮失败回滚本轮 user 消息不退出循环）
- `repl_test.go`: 覆盖会话编排的单元测试（RunOnce 流式输出与 system 携带 / REPL 多轮历史携带 / /clear 清空），假 gateway 回显 messages

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
