# internal/daemon/
> L2 | 父级: ../../CLAUDE.md

外接 brain 的设备接入(agent-design/Design.md §8.1):注册设备、心跳续活、拉取式 claim 领工作、驱动本机 coding CLI 执行并回写事件流。正确性完全建立在拉取式 claim 上,连接断开只影响延迟。功能未稳定,入口命令(makecli daemon)隐藏。

成员清单
protocol.go: daemon 协议 wire 类型(信封/设备/claim/生命周期/事件)——makecli 是公开仓库无法 import 私有 agent-contract,在此镜像线上形状,真相源 agent-design/Contract.md
client.go: gateway 设备面 /v1/daemon/* 的类型化 HTTP client(Bearer token + X-Make-Target + 信封解包),APIError 还原类型化原因
daemon.go: 主循环——注册 → 心跳 goroutine(15s,消费 cancel_run 指令)→ 按 provider 分别 claim 轮询(3s,RunClaim 不带 provider,单能力请求领到即知道用哪个 CLI)→ v1 串行执行
run.go: 单 run 执行编排——start → 读触发区间 → 执行 → 事件攒批上报(batch_seq 单调,模糊重试不双写;中间文本映射 status,最终答复才是 message)→ complete/fail(取消收尾优先)
execenv.go: v1 最小执行环境——工作目录连续性优先,instructions 渲染为 CLAUDE.md + AGENTS.md 双文件;BuildPrompt 合并触发区间的 user_message
daemon_test.go / execenv_test.go: 编排回归(httptest 假 gateway + 桩 backend)与执行环境回归
adapter/: CLI 适配层,见其 CLAUDE.md

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
