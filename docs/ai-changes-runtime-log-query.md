# MakeCLI 运行日志查询

## 2026-11-18 表达式 Trace 查询

- 运行日志查询将 trace_id 放入 filter.expression，与 Console 查询协议一致。
- 涉及 internal/api/runtime_log.go 和 cmd/logs_runtime_test.go。

## 2026-11-18：展示运行日志异常堆栈

- 表格输出在日志内容后展示异常堆栈。
- 增加多行堆栈输出回归测试。
- 涉及 `cmd/logs_runtime.go` 和相关测试。

## 2026-11-18

- 新增 `makecli logs runtime --trace-id`，按链路查询运行日志，不要求应用和时间参数。
- 对接 Console 日志查询的待实现 traceId 契约，按时间正序分页，输出表格或 JSON 并标识未取全结果。
- 涉及 `cmd/logs_runtime.go`、`internal/api/runtime_log.go`、命令测试及使用说明。
