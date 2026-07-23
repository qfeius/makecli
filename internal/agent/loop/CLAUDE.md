# internal/agent/loop/
> L2 | 父级: ../CLAUDE.md

两层 agent 循环 + 系统提示词组装。移植自 github.com/smallnest/pigo internal/runtime 的 loop.go/stream_response.go/prompt.go（MIT License, Copyright (c) 2026 smallnest），裁掉 compaction（自动压缩）与 subagent；依赖 core（类型/事件流）、tool（批量执行）、llm（StreamFn 抽象）。内层循环：流式出一条 assistant → 执行其工具调用 → 结果回喂，直到无工具调用；外层循环：GetFollowUpMessages 有产出则续跑。

## 成员清单
- `loop.go`: RunConfig（LoopConfig + Batch + 四个循环级钩子 GetFollowUpMessages/GetSteeringMessages/PrepareNextTurn/ShouldStopAfterTurn + EventBuffer/SessionID）、TurnUpdate、LoopEventStream、StartRun 导出入口、runLoop 两层循环（length 截断保护 failToolCallsFromTruncatedMessage；error/aborted 即止；全员 terminate 即止）、afterTurn 钩子序
- `stream_response.go`: LoopConfig（Model/APIKey/ThinkingLevel/Stream + TransformContext/ConvertToLlm/GetAPIKey/Provider/Extra）、streamAssistantResponse 单轮流式（transform → convert → 动态 key → 建流 → drain 回填 partial，请求失败一律化为终结 assistant 消息不返回 error）
- `prompt.go`: BuildSystemPrompt 三层组装（DefaultBaseInstruction["You are makecli agent..."] + 环境块[cwd/OS/日期] + Root→WorkingDir 目录链上的 AGENTS.md 由泛到专注入 + AppendInstructions 末尾追加）、PromptConfig（Now/ReadFile 可注入测试）
- `loop_test.go` / `stream_response_test.go` / `prompt_test.go`: 粗粒度 StreamFn 桩驱动的循环单测、单轮流式回填与钩子次序、提示词组装与 AGENTS.md 排序
- `faux_provider_test.go`: fauxProvider——实现 llm.Provider 的细粒度脚本回放假 provider，经 StreamFnFromProvider 接入真实缝合面；是后续 gateway 适配器的行为契约（事件形状、六钩子、截断保护、并行序、取消）
- `testtools_test.go`: execTool/echoTool 可配置假工具

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
