# internal/trace/
> L2 | 父级: /CLAUDE.md

## 职责
出站请求的 W3C Trace Context 头生成。零依赖手写（traceparent v00 格式已冻结，无需引 otel SDK）。

## 核心语义
- **trace-id 进程级单一真相源**：`sync.OnceValue` 懒初始化一次，每次 CLI 调用一个，全程复用 —— 一条命令下发的所有请求被后端串成同一棵 trace 树
- **parent-id 每请求新生成**：`Traceparent()` 每次调用换一个 span 段，标识 trace 树上的节点
- **X-Log-Id = trace-id 段**：`TraceID()` 返回的 32 hex 即 X-Log-Id 的值，与 traceparent 第二段一致，供日志关联
- 全零回退分支被刻意省略（概率 2^-128/2^-64 可忽略），保持无分支

## 成员清单
- `trace.go`: 提供 TraceID()（稳定 trace-id / X-Log-Id 值）、Traceparent()（W3C v00 串，trace-id 稳定 + parent-id 每次新生成，flags 固定 01）；randHex(n) 用 crypto/rand 生成 n 字节并 hex 编码，与 otel randomIDGenerator 同源
- `trace_test.go`: 覆盖 trace-id 稳定性与 32-hex 格式、traceparent v00 格式锚定、trace-id 跨请求复用、parent-id 每次新生成

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
