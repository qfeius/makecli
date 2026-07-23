# internal/agent/llm/
> L2 | 父级: ../CLAUDE.md

loop 所依赖的 LLM 流式抽象（纯类型，零实现）。类型取自 github.com/smallnest/pigo internal/provider 中循环所需的最小集合（MIT License, Copyright (c) 2026 smallnest）；pigo 的 transport/openai/auth/registry/presets 等厂商实现不移植——指向 makecli gateway 的适配器（实现 StreamFn）由后续任务在本包落地。双失败契约：建流失败才返回 error，运行期失败一律走流上的终结 error 事件（stopReason=error/aborted）。

## 成员清单
- `types.go`: AssistantMessageEvent 密封接口 + 六种增量事件（Start/Text/Thinking/ToolCall/Done/Error）、AssistantMessageEventStream（core.EventStream 特化）与 NewAssistantMessageEventStream（done/error 自动收尾）、LlmContext、StreamConfig、StreamFn、Model（能力元数据）、CompletionRequest、Provider 接口、StreamFnFromProvider 适配

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
