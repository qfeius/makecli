/**
 * [INPUT]: 依赖 context、fmt、maps、slices、strings
 * [OUTPUT]: 对外提供 Remove / RemoveCommand，删除已安装的 Make platform skills
 * [POS]: internal/skillsync 的删除层，被 cmd/skills_remove.go 消费；来源校验挡住误删第三方 skills；复用 sync.go 的 runSkillsCommand seam 与 syncTimeout
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package skillsync

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// RemoveCommand 返回删除指定 skills 的非交互命令。
func RemoveCommand(names []string) []string {
	command := []string{"npx", "-y", "skills", "remove"}
	command = append(command, names...)
	return append(command, "-y")
}

// Remove 删除指定的 Make platform skills。
// 名字必须都是 lockfile 中 source == SkillsSource 的已安装 skill——
// 用户机器上可能有几十个第三方 skills，makecli 不越界删除。
func Remove(ctx context.Context, names []string) error {
	installed, _ := readLock()

	var invalid []string
	for _, name := range names {
		if _, ok := installed[name]; !ok {
			invalid = append(invalid, name)
		}
	}
	if len(invalid) > 0 {
		hint := "(none installed)"
		if candidates := slices.Sorted(maps.Keys(installed)); len(candidates) > 0 {
			hint = strings.Join(candidates, ", ")
		}
		return fmt.Errorf("not installed Make platform skills: %s\ninstalled Make platform skills: %s",
			strings.Join(invalid, ", "), hint)
	}

	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()

	command := RemoveCommand(names)
	output, err := runSkillsCommand(runCtx, command)
	if err != nil {
		return fmt.Errorf("failed to remove skills: %w\nmanual fix: %s\n%s",
			err, strings.Join(command, " "), trimOutput(strings.TrimSpace(output)))
	}
	return nil
}
