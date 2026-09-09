/**
 * [INPUT]: 依赖 fmt、io/fs、path、strings
 * [OUTPUT]: 对外提供 Read（按 "<skill>[/<path>]" 读文件或列目录）、Names（嵌入的 skill 名清单）、Result
 * [POS]: skillcontent 模块的读取层，纯函数作用于任意 fs.FS（生产用 embed.go 的 FS，测试注入 fstest.MapFS）；被 cmd/skills_read 消费
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package skillcontent

import (
	"fmt"
	"io/fs"
	"path"
	"strings"
)

// MainFile 是 target 只给 skill 名时的默认读取目标。
const MainFile = "SKILL.md"

// Result 是一次 Read 的产物：Content 与 Entries 恰好一个非空——
// 目标是文件则 Content 为字节原文，目标是目录则 Entries 为子项名（子目录带尾斜杠）。
type Result struct {
	Skill   string
	Path    string // 相对 skill 根的路径，主文件为 MainFile
	Content []byte
	Entries []string
}

// IsMain 报告本次读的是否是 SKILL.md 主文件。
func (r Result) IsMain() bool { return r.Path == MainFile }

// Names 返回 fsys 根下的 skill 名（按 ReadDir 的字典序）。
func Names(fsys fs.FS) []string {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	return names
}

// Read 解析 "<skill>[/<path>]" 并读取：path 缺省为 SKILL.md；目标是目录则列一层子项。
// 路径先 path.Clean 再交给 fs.FS——fs.ValidPath 拒绝 ".." 与绝对路径，
// 逃逸不可能发生，这里只负责把拒绝翻译成可读错误。
func Read(fsys fs.FS, target string) (Result, error) {
	target = path.Clean(target)
	name, rel, _ := strings.Cut(target, "/")
	if !fs.ValidPath(target) || name == "." {
		return Result{}, unknownSkill(fsys, name)
	}
	if info, err := fs.Stat(fsys, name); err != nil || !info.IsDir() {
		return Result{}, unknownSkill(fsys, name)
	}
	if rel == "" {
		rel = MainFile
	}

	full := name + "/" + rel
	info, err := fs.Stat(fsys, full)
	if err != nil {
		top, _ := listDir(fsys, name)
		return Result{}, fmt.Errorf("%s not found in skill %s; top-level entries: %s", rel, name, strings.Join(top, ", "))
	}
	res := Result{Skill: name, Path: rel}
	if info.IsDir() {
		res.Entries, err = listDir(fsys, full)
		return res, err
	}
	res.Content, err = fs.ReadFile(fsys, full)
	return res, err
}

func listDir(fsys fs.FS, dir string) ([]string, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		suffix := ""
		if e.IsDir() {
			suffix = "/"
		}
		names = append(names, e.Name()+suffix)
	}
	return names, nil
}

func unknownSkill(fsys fs.FS, name string) error {
	return fmt.Errorf("unknown skill %q; embedded skills: %s", name, strings.Join(Names(fsys), ", "))
}
