# agents/
> L2 | 父级: /CLAUDE.md

## 成员清单
- `embed.go`: 通过 //go:embed 把脚手架模板编译进二进制，导出 Templates embed.FS，供 cmd/app_create（写文档）与 cmd/git ensureGitignore（读 .gitignore 期望清单）消费
- `CLAUDE.md.tmpl`: 脚手架模板——写入用户项目根目录的 CLAUDE.md（内容 `@AGENTS.md`，用 import 指令引同级 AGENTS.md）
- `AGENTS.md.tmpl`: 脚手架模板——写入用户项目的 AGENTS.md，定义 Make 平台 app 开发的 agent 身份 / 工作流 / 目录结构 / 构建契约
- `gitignore.tmpl`: .gitignore 期望条目的**单一真相源**（node_modules / 构建产物 / .env 密钥 / OS 文件）——cmd/git ensureGitignore 据此幂等补齐用户项目 .gitignore（缺失写全文，已存在仅追加缺条目），由 app init 与 app create 的 scaffoldGit 消费；保证 deploy 的 git add -A 不吞非源码文件

## 命名约定
- 模板源文件用 `.tmpl` 后缀，避免与 GEB 的 `CLAUDE.md`（L2 文档约定）撞名导致 lint 误判
- cmd/app_create 用 scaffoldFile{embed,out} 映射文档模板名到写出名：CLAUDE.md.tmpl→CLAUDE.md、AGENTS.md.tmpl→AGENTS.md（前导点不能进 //go:embed，故源文件名不带点）
- gitignore.tmpl 不走 scaffoldFile——由 ensureGitignore 解析其有意义行作为期望条目增量补齐，而非整文件写出

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
