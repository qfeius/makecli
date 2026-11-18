# MakeCLI 运行日志查询

## 2026-11-18

- 新增 `makecli logs runtime --trace-id`，按链路查询运行日志，不要求应用和时间参数。
- 对接 Console 日志查询的待实现 traceId 契约，按时间正序分页，输出表格或 JSON 并标识未取全结果。
- 涉及 `cmd/logs_runtime.go`、`internal/api/runtime_log.go`、命令测试及使用说明。
