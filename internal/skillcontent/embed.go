/**
 * [INPUT]: 依赖 embed、io/fs；引用同目录 git submodule make-platform-skills/（上游 qfeius/make-platform-skills，超项目钉住 commit）
 * [OUTPUT]: 对外提供 FS（fs.FS，以 skill 名为根：makeui/SKILL.md、makeui/references/x.md）
 * [POS]: skillcontent 模块的 embed 文件，把上游 skills 的 agent 可读内容（SKILL.md + references/）编译进二进制，供 cmd/skills_read 消费；scripts/ agents/ 是机器资源不嵌入
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package skillcontent

import (
	"embed"
	"io/fs"
)

// 白名单式嵌入：新的内容类型须显式加入 pattern 才会随二进制发布。
// submodule 未初始化时这里编译失败（pattern 无匹配），这是有意的——强制先 `make sync`。
//
//go:embed make-platform-skills/skills/*/SKILL.md make-platform-skills/skills/*/references
var upstream embed.FS

// FS 以 skill 名为根，去掉 submodule 的路径前缀。
var FS = mustSub(upstream, "make-platform-skills/skills")

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic("skillcontent: " + err.Error())
	}
	return sub
}
