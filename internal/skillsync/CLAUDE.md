# internal/skillsync/
> L2 | 父级: /CLAUDE.md

## 成员清单
sync.go:           Make platform skills 同步编排层，默认每次执行 `npx -y skills add qfeius/make-platform-skills --all -y`；Options 控制 Version/Skip，Result 给 cmd/update.go 渲染用户可见输出；runSkillsCommand 包级 seam 隔离 npx 副作用并带 3 分钟超时
sync_test.go:      覆盖每次同步都执行命令、--skip-skills 跳过、命令失败时输出手动修复命令；白盒替换 runSkillsCommand，避免真实执行 npx
inventory.go:      清单层——本地半边（lockfile 读取过滤 Make platform skills + SKILL.md 描述解析：readLock/readDescription/extractFrontmatter）+ 远端半边（GitHub Contents API 拉 skill 目录 tree SHA：fetchRemoteSkills）+ List 合并两者产出排序后的 Inventory{Skills []SkillInfo, LockWarning, RemoteErr}；Status* 常量描述本地×远端比对结果；lockPathFunc/skillsDirFunc/inventoryAPIBaseURL 为测试接缝
inventory_test.go: 覆盖 readLock（缺失/过滤/损坏/版本不匹配）、extractFrontmatter/readDescription、fetchRemoteSkills（正常/HTTP 错误）、List（状态合并/远端下架/远端不可达降级 unknown/已装条目补 description）；stubLockFile/stubSkillsDir/stubRemoteAPI 隔离文件系统与网络

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
