# internal/agent/core/
> L2 | 父级: ../CLAUDE.md

agent 内核的叶子数据类型与控制流原语，纯 stdlib 零依赖，agent 其余子包（tool/llm/loop）都依赖它、它不依赖它们。整包移植自 github.com/smallnest/pigo internal/agentcore（MIT License, Copyright (c) 2026 smallnest），保语义改包路径，pigo 原注释保留。

## 成员清单
- `content.go`: Content 密封接口 + 四种内容块（TextContent/ThinkingContent/ToolCallContent/ImageContent）、构造器、ContentList（按 "type" 判别式解码的自定义 UnmarshalJSON）
- `message.go`: Message 密封接口 + 四种角色（UserMessage/AssistantMessage/ToolResultMessage/CompactionMessage）、Usage、StopReason 常量、MessageList（按 "role" 判别式解码）；CompactionMessage 仅作数据类型保留（compaction 逻辑未移植）
- `event.go`: AgentEvent 密封接口 + 11 种循环事件（agent_start/end、turn_start/end、message_start/update/end、tool_execution_start/update/end、compaction）
- `event_stream.go`: EventStream[T,R] 泛型流（channel 迭代 + Result 终值 + ctx 取消；IsComplete/ExtractResult 可选自动收尾）、ErrStreamIncomplete
- `tool.go`: AgentContext、AgentTool 接口（Name/Description/Schema/ExecutionMode/Execute）、AgentToolCall、AgentToolResult、ToolExecutionMode（parallel/sequential）、ToolUpdateFunc
- `hooks.go`: ThinkingLevel 六级枚举与 ThinkingLevelMap、AfterToolCallResult、AgentLoopTurnUpdate（指针字段区分「未提供」与显式零值）
- `helpers.go`: ContentToText、LastAssistantOf、EmitFunc、PrepareArgumentsFunc、BeforeToolCallDecision/BeforeToolCallFunc、AfterToolCallFunc
- `event_stream_test.go` / `helpers_test.go` / `types_test.go`: 事件流收尾语义、JSON 判别式编解码回环、hooks 指针语义的单测

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
