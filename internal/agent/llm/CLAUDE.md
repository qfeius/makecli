# internal/agent/llm/
> L2 | 父级: ../CLAUDE.md

loop 所依赖的 LLM 流式抽象 + 唯一具体 Provider（gateway 适配器）。类型取自 github.com/smallnest/pigo internal/provider 中循环所需的最小集合（MIT License, Copyright (c) 2026 smallnest）；gateway.go 移植 pigo 的 encodeOpenAIRequest 族与 OpenAIDecoder（剥多模态 image 分支与看门狗/重试传输，按 client.go 简洁风格重写传输）。双失败契约：建流前失败（编码/传输/非 200）才返回 error，流开始后的失败一律走流上的终结 error 事件（stopReason=error/aborted）。

## 成员清单
- `types.go`: AssistantMessageEvent 密封接口 + 六种增量事件（Start/Text/Thinking/ToolCall/Done/Error）、AssistantMessageEventStream（core.EventStream 特化）与 NewAssistantMessageEventStream（done/error 自动收尾）、LlmContext、StreamConfig、StreamFn、Model（能力元数据）、CompletionRequest、Provider 接口、StreamFnFromProvider 适配
- `gateway.go`: GatewayProvider（llm.Provider 实现：OpenAI 兼容 SSE 指向 gateway /v1/chat/completions，头部同 client.go——Bearer token + X-Session-ID；StreamConfig.APIKey 非空时覆盖构造 token）、encodeGatewayRequest/Message/Tools（system+messages[assistant 展开 content+tool_calls、tool 结果→role:"tool"]+stream:true+include_usage+有工具才加 tools）、gatewayDecoder（choice.delta 累积 text/tool_calls 分片、finish_reason→StopReason 映射、usage 采集、[DONE]/EOF 均 finishDone 收尾不丢部分响应、坏 chunk/断流→StreamErrorEvent[ctx 取消为 aborted]）、decodeGatewayAPIError（OpenAI 风格错误体，同 client.go decodeAPIError 逻辑）
- `gateway_test.go`: httptest SSE 桩覆盖——纯文本流（头部/请求体/usage/stop 映射）、tool_calls 跨 chunk 分片拼装、多轮历史与工具声明的线缆形状、401 错误体返回 Go error、[DONE] 缺失 EOF 收尾、流中坏 chunk 化为 StreamErrorEvent

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
