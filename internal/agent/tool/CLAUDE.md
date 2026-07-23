# internal/agent/tool/
> L2 | 父级: ../CLAUDE.md

agent 的内建工具集与三段式执行器。移植自 github.com/smallnest/pigo internal/agenttool（MIT License, Copyright (c) 2026 smallnest），裁掉 todo/webfetch/htmlmarkdown；每个失败模式都编码为 IsError 工具结果而非 Go error，模型永远拿得到可反馈的 ToolResultMessage。唯一外部依赖 github.com/santhosh-tekuri/jsonschema/v6（参数校验）。

## 成员清单
- `registry.go`: ToolRegistry（按名注册 + 注册期编译 JSON Schema + Validate 展平字段级错误 FieldError）、ValidationErrorResult
- `tool_executor.go`: executeToolCall 三段式（prepare[查表/prepareArguments/校验/beforeToolCall 拦截] → execute[panic 兜底 + onUpdate 流式] → finalize[afterToolCall 字段级覆盖 + 事件]）、decodeArgs 泛型参数解码、errorResult/errorToolResult
- `batch_executor.go`: ExecuteToolCalls 批量执行（任一工具声明 sequential 或 ForceSequential 则整批串行，否则并行 + 按源序索引回填；全员 terminate 才终止 run）、BatchConfig
- `read_tool.go`: ReadTool（行号输出、offset/limit 分页、2000 行截断、超长行截断）；filePerm/dirPerm、scanner 缓冲常量
- `write_tool.go`: WriteTool（建父目录、覆盖检测上报，sequential）
- `edit_tool.go`: EditTool(精确串替换、唯一性检查、replace_all；LCS 行 diff 返回)
- `search_tool.go`: resolveWithin（全体文件工具共用的 workspace 越界防护单点）、gitignore 最小匹配器（锚定/目录/取反/段匹配）、GrepTool/FindTool/LsTool（只读 parallel，1000 条截断）
- `bash_tool.go`: BashTool（bash -c 执行、stdout/stderr 合流经 onUpdate 增量上报、默认 2min/上限 10min 超时、非零退出码入 error）
- `*_test.go`: 各文件对应单测（registry 校验/执行器钩子与 panic/批量顺序与终止/各工具行为与越界防护）

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
