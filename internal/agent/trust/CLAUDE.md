# internal/agent/trust/
> L2 | 父级: ../CLAUDE.md

按目录持久化的信任决策存储（副作用工具 bash/write/edit 执行前的授权依据），纯 stdlib + internal/config。移植自 github.com/smallnest/pigo internal/trust（MIT License, Copyright (c) 2026 smallnest）；持久化路径改 makecli 惯例：`config.Dir()`（默认 ~/.make，受 MAKE_CLI_CONFIG_DIR 覆盖）下的 agent-trust.json。仅存取决策，交互提示与权限钩子在上层。

## 成员清单
- `manager.go`: Decision 三态（Trusted/Untrusted/Undecided ↔ JSON bool|null）、Manager（NewManager 加载：缺文件不报错、坏文件硬错；NearestTrustDecision 最近祖先查找；IsTrusted 会话优先于持久；SetDecision/Forget 原子写盘 CreateTemp+Rename；SetSessionTrust/ClearSessionTrust 进程内一次性授权）、DefaultPath
- `manager_test.go`: 三态回环、祖先遮蔽、会话不落盘、并发安全、排序稳定输出、DefaultPath 的 MAKE_CLI_CONFIG_DIR 覆盖语义

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
