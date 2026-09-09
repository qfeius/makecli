# internal/skillcontent/
> L2 | 父级: /CLAUDE.md

## 成员清单
- `make-platform-skills/`: git submodule → github.com/qfeius/make-platform-skills（超项目钉住 commit，`make sync` 拉齐）；内容单一真相源，本仓库不改其文件；跟踪 main（.gitmodules branch），发版前由 /ship Step 0 `git submodule update --remote` bump 并单独提交，保证每个发布版本 = 打 tag 时的上游 main 且 tag 记录确切 sha
- `embed.go`: 白名单 `//go:embed` 嵌入 submodule 的 `skills/*/SKILL.md` 与 `skills/*/references`（scripts/ agents/ 是机器资源不嵌入），fs.Sub 去前缀后导出 FS（以 skill 名为根）；submodule 未初始化则编译失败——有意为之，强制先 `make sync`
- `reader.go`: 读取层——Read(fsys, "<skill>[/<path>]") 解析目标：path 缺省 SKILL.md、目标是目录则列一层子项（Result.Content / Entries 二选一）；未知 skill 报错附嵌入清单、路径未找到报错附 skill 顶层条目（错误自带导航，不设单独 list 语法）；逃逸由 fs.ValidPath 兜底；Names 列嵌入的 skill 名
- `reader_test.go`: fstest.MapFS 覆盖主文件缺省/子文件/目录列举/未知 skill/未找到/逃逸拒绝；TestEmbeddedFS 冒烟真实 FS（makeui 存在、带 frontmatter、references 非空），钉住 embed 通路

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
