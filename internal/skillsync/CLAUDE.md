# internal/skillsync/
> L2 | 父级: /CLAUDE.md

## 成员清单
sync.go:           Make platform skills 同步编排层，默认每次执行 `npx -y skills add qfeius/make-platform-skills --all -y`；Options 控制 Version/Skip，Result 给 cmd/update.go 渲染用户可见输出；runSkillsCommand 包级 seam 隔离 npx 副作用并带 3 分钟超时
sync_test.go:      覆盖每次同步都执行命令、--skip-skills 跳过、命令失败时输出手动修复命令；白盒替换 runSkillsCommand，避免真实执行 npx
inventory.go:      本地清单层——lockfile 读取（过滤 Make platform skills）+ SKILL.md 描述解析；readLock/readDescription/extractFrontmatter；lockPathFunc/skillsDirFunc 为测试接缝
inventory_test.go: 覆盖 readLock（缺失/过滤/损坏/版本不匹配）与 extractFrontmatter/readDescription；stubLockFile/stubSkillsDir 隔离文件系统

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
