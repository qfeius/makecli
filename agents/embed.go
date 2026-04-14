/**
 * [INPUT]: 依赖 embed 包，引用同目录 CLAUDE.md 和 AGENTS.md 模板文件
 * [OUTPUT]: 对外提供 Templates embed.FS，包含 app init 所需的模板文件
 * [POS]: agents 模块唯一文件，将模板编译进二进制供 app init 写出
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package agents

import "embed"

//go:embed CLAUDE.md AGENTS.md
var Templates embed.FS
