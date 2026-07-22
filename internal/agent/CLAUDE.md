# internal/agent/
> L2 | 父级: /CLAUDE.md

keyless 本地 agent（隐藏命令 `makecli agent` 的执行层，agent-design/Design.md §8.2）：标准 OpenAI 兼容客户端指向 gateway `/v1/chat/completions`，模型名用平台别名，配额与五维计量在模型面自动生效——设备端零厂商 key。v1 纯聊天（流式 REPL + 一次性模式）；loop / tools / context / session 四模块与 bubbletea TUI 随后续 goal 进入。

## 成员清单
- `client.go`: Client（ChatStream：SSE 流式补全，增量回调 + 全文返回）、Message、NewSessionID（X-Session-ID 计量维度，无安全语义）、APIError（OpenAI 风格错误体还原，解不出保底状态码+原文摘要）
- `client_test.go`: 覆盖 ChatStream 的单元测试（SSE 增量解析与 headers / OpenAI 错误体还原 / session id 随机性），httptest 隔离网络
- `repl.go`: RunOnce（单条 prompt 流式打印）与 RunREPL（读一行发一轮，历史进程内存续；/exit /quit 退出、/clear 清空历史；单轮失败回滚本轮 user 消息不退出循环）
- `repl_test.go`: 覆盖会话编排的单元测试（RunOnce 流式输出与 system 携带 / REPL 多轮历史携带 / /clear 清空），假 gateway 回显 messages

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
