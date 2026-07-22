# internal/daemon/adapter/
> L2 | 父级: ../CLAUDE.md

外接 brain CLI 的统一执行契约:单方法 Backend.Execute(ctx, prompt, opts) → Session{Messages, Result}。CLI 差异止步于各实现文件,包外不出现任何 CLI 专有词汇。

成员清单
adapter.go: Backend 接口(Execute/Provider/Detect)、ExecOptions(WorkDir/ResumeSessionID/MaxRunDuration)、Session、Message 统一事件、Result、TokenUsage
claudecode.go: claude-code 实现——`claude -p <prompt> --output-format stream-json --verbose --permission-mode bypassPermissions [--resume]`,逐行归一;parseClaudeLine 是解析锚点,未知行静默跳过
codex.go: codex 实现——长驻 `codex app-server --listen stdio://` JSON-RPC:initialize → thread/start|resume(失败回退新线程)→ turn/start;codexRPC 精简客户端(应答配对/通知归一/审批自动放行);stdout 关闭触发终局兜底
claudecode_test.go: stream-json 解析 golden 回归
codex_test.go: JSON-RPC 语义回归(io.Pipe 模拟 app-server,不依赖真实二进制)

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
