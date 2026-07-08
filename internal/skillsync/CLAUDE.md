# internal/skillsync/
> L2 | 父级: /CLAUDE.md

## 成员清单
sync.go:           同步层——默认每次执行 `npx -y skills add qfeius/make-platform-skills --all -y`；Options 控制 Version/Skip，Result 给 cmd/update.go 渲染用户可见输出；runSkillsCommand 包级 seam 隔离 npx 副作用并带 3 分钟超时（syncTimeout 与 trimOutput 被 remove.go 复用）
sync_test.go:      覆盖每次同步都执行命令、--skip-skills 跳过、命令失败时输出手动修复命令；白盒替换 runSkillsCommand，避免真实执行 npx
inventory.go:      清单层——本地半边（lockfile 读取过滤 Make platform skills + SKILL.md 描述解析：readLock/readDescription/extractFrontmatter）+ 远端半边（GitHub Contents API 拉 skill 目录 tree SHA：fetchRemoteSkills）+ List 合并两者产出排序后的 Inventory{Skills []SkillInfo, LockWarning, RemoteErr}；Status* 常量描述本地×远端比对结果；lockPathFunc/skillsDirFunc/inventoryAPIBaseURL 为测试接缝
inventory_test.go: 覆盖 readLock（缺失/过滤/损坏/版本不匹配）、extractFrontmatter/readDescription、fetchRemoteSkills（正常/HTTP 错误）、List（状态合并/远端下架/远端不可达降级 unknown/已装条目补 description）；stubLockFile/stubSkillsDir/stubRemoteAPI 隔离文件系统与网络
remove.go:         删除层——RemoveCommand 返回非交互 npx 删除命令，Remove 读 lockfile 验证所有 skill 都来自 SkillsSource 挡住误删第三方 skill，校验通过后执行删除；复用 readLock / runSkillsCommand / syncTimeout / trimOutput
remove_test.go:    覆盖 RemoveCommand 构造、Remove 的来源校验/正常执行/第三方 skill 拒绝/未安装名称拒绝/空 lockfile/命令失败等路径；stubRunSkillsCommand 隔离 npx 执行

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
