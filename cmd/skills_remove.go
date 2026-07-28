/**
 * [INPUT]: 依赖 context、errors、fmt、os、strings、charm.land/huh/v2、github.com/mattn/go-isatty、github.com/spf13/cobra、internal/skillsync
 * [OUTPUT]: 对外提供 newSkillsRemoveCmd 函数；包级 planRemoveFunc / removeSkillsFunc / confirmRemoveFunc 可打桩变量
 * [POS]: cmd/skills 的 remove 子命令：按名移除 / --all 全量互斥，缺省 huh confirm 确认（--yes 跳过，非 TTY 拒绝并指引），两阶段调用 skillsync.PlanRemove → Remove，逐项渲染结果
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"charm.land/huh/v2"
	"github.com/mattn/go-isatty"
	"github.com/qfeius/makecli/internal/skillsync"
	"github.com/spf13/cobra"
)

// planRemoveFunc / removeSkillsFunc / confirmRemoveFunc 为包级可打桩变量，
// 单测替换以隔离 lockfile、npx 执行与终端交互（参照 skills_install.go 模式）。
var planRemoveFunc = skillsync.PlanRemove
var removeSkillsFunc = skillsync.Remove
var confirmRemoveFunc = confirmRemove

func newSkillsRemoveCmd() *cobra.Command {
	var all, yes bool

	cmd := &cobra.Command{
		Use:   "remove [name]...",
		Short: "Remove installed Make platform skills",
		Example: `  makecli skills remove makedsl makeui    # 按名移除
  makecli skills remove --all             # 移除全部已装 Make platform skills
  makecli skills remove --all -y          # 跳过确认（CI / 非交互）`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillsRemove(cmd.Context(), cmd, args, all, yes)
		},
	}

	cmd.Flags().BoolVarP(&all, "all", "a", false, "remove all installed Make platform skills")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	return cmd
}

func runSkillsRemove(ctx context.Context, cmd *cobra.Command, names []string, all, yes bool) error {
	if all && len(names) > 0 {
		return errors.New("cannot use --all with skill names")
	}
	if !all && len(names) == 0 {
		return errors.New("specify skill names or --all (run 'makecli skills list' to see what's installed)")
	}

	plan, err := planRemoveFunc(names, all)
	if err != nil {
		return err
	}
	if plan.Warning != "" {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", plan.Warning)
	}

	if !yes {
		if err := confirmRemoveFunc(plan); err != nil {
			return err
		}
	}

	results, err := removeSkillsFunc(ctx, plan)
	var removed []string
	for _, r := range results {
		if r.Err == nil {
			removed = append(removed, r.Name)
		} else {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "failed %s: %v\n", r.Name, r.Err)
		}
	}
	if len(removed) > 0 {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed: %s\n", strings.Join(removed, ", "))
	}
	return err
}

// confirmRemove 在执行前确认移除计划（skills install 同款 huh confirm 护栏）。
// 非交互终端（管道 / CI）无法确认，直接拒绝并指引 --yes，杜绝挂起。
func confirmRemove(plan skillsync.RemovePlan) error {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return errors.New("refusing to remove without confirmation: re-run with --yes in a non-interactive shell")
	}

	confirmed := false
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Remove Make platform skills?").
				Description(fmt.Sprintf("Source: %s\nSkills: %s\nTarget: all detected code agents",
					skillsync.SkillsSource, strings.Join(plan.Names, ", "))).
				Affirmative("Remove").
				Negative("Abort").
				Value(&confirmed),
		),
	).Run()

	if errors.Is(err, huh.ErrUserAborted) || (err == nil && !confirmed) {
		return errors.New("remove cancelled")
	}
	return err
}
