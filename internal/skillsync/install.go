/**
 * [INPUT]: 依赖 context、fmt、maps、slices、strings
 * [OUTPUT]: 对外提供 PlanInstall / Install / InstallPlan / InstallCommand，安装指定或全部 Make platform skills
 * [POS]: internal/skillsync 的安装层，被 cmd/skills_install.go 消费；两阶段：PlanInstall（EnsureNpx 门禁 + 远端校验 + 构造命令）供 cmd 层确认展示，Install 执行；复用 sync.go 的 runSkillsCommand / syncTimeout / trimOutput 与 inventory.go 的 fetchRemoteSkills
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

// InstallPlan 是一次安装的已解析计划：cmd 层拿它渲染确认提示，确认后原样交给 Install 执行。
type InstallPlan struct {
	Names   []string // 将安装的 skill 名；--all 且远端不可达时为空（装什么由 npx 裁决）
	All     bool
	Command []string // 将执行的 npx 命令
	Warning string   // 远端不可达降级时的提示，cmd 层渲染到 stderr
}

// InstallCommand 返回安装命令：--all 与 update 同一条命令（同一语义）；
// 按名走上游 -s（单 flag 贪婪收集空格分隔多值）+ -a '*'（全部 agent，与 --all 行为一致）。
func InstallCommand(names []string, all bool) []string {
	if all {
		return SkillsCommand()
	}
	command := []string{"npx", "-y", "skills", "add", SkillsSource, "-s"}
	command = append(command, names...)
	return append(command, "-a", "*", "-y")
}

// PlanInstall 解析一次安装：npx 环境门禁 → 远端清单校验/展开 → 构造命令。
// 按名拼错即报错并列出可用名字；远端不可达是降级不是门禁（Warning 提示，npx 裁决）。
func PlanInstall(ctx context.Context, names []string, all bool) (InstallPlan, error) {
	if err := EnsureNpx(); err != nil {
		return InstallPlan{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}

	plan := InstallPlan{All: all}
	remote, err := fetchRemoteSkills(ctx)
	switch {
	case all && err == nil:
		plan.Names = slices.Sorted(maps.Keys(remote))
	case all:
		plan.Warning = fmt.Sprintf("cannot list remote skills: %v", err)
	case err != nil:
		plan.Names = names
		plan.Warning = fmt.Sprintf("cannot verify skill names against %s: %v", SkillsSource, err)
	default:
		var invalid []string
		for _, name := range names {
			if _, ok := remote[name]; !ok {
				invalid = append(invalid, name)
			}
		}
		if len(invalid) > 0 {
			return InstallPlan{}, fmt.Errorf("unknown Make platform skills: %s\navailable skills: %s",
				strings.Join(invalid, ", "), strings.Join(slices.Sorted(maps.Keys(remote)), ", "))
		}
		plan.Names = names
	}
	plan.Command = InstallCommand(names, all)
	return plan, nil
}

// Install 执行已确认的安装计划。
func Install(ctx context.Context, plan InstallPlan) error {
	if ctx == nil {
		ctx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(ctx, syncTimeout)
	defer cancel()

	output, err := runSkillsCommand(runCtx, plan.Command)
	if err != nil {
		return fmt.Errorf("failed to install skills: %w\nmanual fix: %s\n%s",
			err, strings.Join(plan.Command, " "), trimOutput(strings.TrimSpace(output)))
	}
	return nil
}
