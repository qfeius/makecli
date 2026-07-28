/**
 * [INPUT]: 依赖 context、errors、fmt、maps、slices、strings
 * [OUTPUT]: 对外提供 PlanRemove / Remove / RemovePlan / RemoveResult / RemoveCommand，移除已安装的 Make platform skills
 * [POS]: internal/skillsync 的删除层，被 cmd/skills_remove.go 消费；两阶段：PlanRemove（EnsureNpx 门禁 + lockfile 校验/展开）供 cmd 层确认展示，Remove 逐个执行（每 skill 一次 npx 调用、独立超时、失败不中断）；--all 从 lockfile 展开为按名删除，绝不透传上游 --all（会误删第三方 skills）；按名清单去重排序，并统一拒绝 flag 形状名字（防投毒 lockfile）；复用 sync.go 的 runSkillsCommand / syncTimeout / trimOutput / dedupSortedNames
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package skillsync

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// RemovePlan 是一次移除的已解析计划：cmd 层拿它渲染确认提示，确认后原样交给 Remove 执行。
type RemovePlan struct {
	Names   []string // 校验/展开后的待删清单（lockfile 为准，已去重排序）
	All     bool
	Warning string // lockfile 损坏等降级警告，cmd 层渲染 stderr
}

// RemoveResult 是单个 skill 的移除结果。
type RemoveResult struct {
	Name string
	Err  error // nil = 已移除；失败含 manual fix 与截断输出
}

// RemoveCommand 返回删除单个 skill 的非交互命令。
func RemoveCommand(name string) []string {
	return []string{"npx", "-y", "skills", "remove", name, "-y"}
}

// PlanRemove 解析一次移除：npx 环境门禁 → lockfile 校验按名 / 展开 --all。
// 名字必须都是 lockfile 中 source == SkillsSource 的已装 skill——
// 用户机器上可能有几十个第三方 skills，makecli 不越界删除；
// --all 同理从 lockfile 展开为按名清单，绝不透传上游 --all。
func PlanRemove(names []string, all bool) (RemovePlan, error) {
	if err := EnsureNpx(); err != nil {
		return RemovePlan{}, err
	}

	installed, warning := readLock()
	plan := RemovePlan{All: all, Warning: warning}

	if all {
		if len(installed) == 0 {
			return RemovePlan{}, errors.New("no Make platform skills installed")
		}
		plan.Names = slices.Sorted(maps.Keys(installed))
	} else {
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
			if warning != "" {
				hint += fmt.Sprintf(" (warning: %s)", warning)
			}
			return RemovePlan{}, fmt.Errorf("not installed Make platform skills: %s\ninstalled Make platform skills: %s",
				strings.Join(invalid, ", "), hint)
		}
		plan.Names = dedupSortedNames(names)
	}

	// 防御性收口：lockfile 或用户输入若混入形如 "--all" 的名字，
	// 绝不能被原样透传给上游 npx 命令。
	for _, name := range plan.Names {
		if strings.HasPrefix(name, "-") {
			return RemovePlan{}, fmt.Errorf("invalid skill name: %q", name)
		}
	}
	return plan, nil
}

// Remove 逐个执行已确认的移除计划：每个 skill 一次 npx 调用、独立超时，
// 单个失败不中断后续，逐项结果交 cmd 层渲染；计划必须来自 PlanRemove（EnsureNpx 门禁在 Plan 层）。
func Remove(ctx context.Context, plan RemovePlan) ([]RemoveResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	results := make([]RemoveResult, 0, len(plan.Names))
	failed := 0
	for _, name := range plan.Names {
		err := removeOne(ctx, name)
		results = append(results, RemoveResult{Name: name, Err: err})
		if err != nil {
			failed++
		}
	}
	if failed > 0 {
		return results, fmt.Errorf("failed to remove %d of %d skills", failed, len(plan.Names))
	}
	return results, nil
}

// removeOne 删除单个 skill，带独立超时。
func removeOne(ctx context.Context, name string) error {
	runCtx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()

	command := RemoveCommand(name)
	output, err := runSkillsCommand(runCtx, command)
	if err != nil {
		return fmt.Errorf("failed to remove %s: %w\nmanual fix: %s\n%s",
			name, err, strings.Join(command, " "), trimOutput(strings.TrimSpace(output)))
	}
	return nil
}
