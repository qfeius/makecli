# agents/
> L2 | 父级: /CLAUDE.md

## 成员清单
embed.go:        通过 //go:embed 把 app init 模板编译进二进制，导出 Templates embed.FS，供 cmd/app_init 写出到用户项目
CLAUDE.md.tmpl:  app init 模板——写入用户项目根目录的 CLAUDE.md（内容 `@AGENTS.md`，用 import 指令引同级 AGENTS.md）
AGENTS.md.tmpl:  app init 模板——写入用户项目的 AGENTS.md，定义 Make 平台 app 开发的 agent 身份 / 工作流 / 目录结构 / 构建契约

## 命名约定
模板源文件用 `.tmpl` 后缀，避免与 GEB 的 `CLAUDE.md`（L2 文档约定）撞名导致 lint 误判；
cmd/app_init 读取时按 `<name>.tmpl` 取模板、去掉 `.tmpl` 后缀写出（CLAUDE.md / AGENTS.md）

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
