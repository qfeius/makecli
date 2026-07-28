/**
 * [INPUT]: 依赖 context、errors、fmt、os、strings、charm.land/huh/v2、github.com/mattn/go-isatty、github.com/spf13/cobra、internal/skillsync
 * [OUTPUT]: 对外提供 newSkillsInstallCmd 函数；包级 planInstallFunc / installSkillsFunc / confirmInstallFunc 可打桩变量
 * [POS]: cmd/skills 的 install 子命令：按名选装 / --all 全量互斥，缺省 huh confirm 确认（--yes 跳过，非 TTY 拒绝并指引），两阶段调用 skillsync.PlanInstall → Install
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

// planInstallFunc / installSkillsFunc / confirmInstallFunc 为包级可打桩变量，
// 单测替换以隔离网络、npx 执行与终端交互（参照 skills_uninstall.go uninstallSkillsFunc 模式）。
var planInstallFunc = skillsync.PlanInstall
var installSkillsFunc = skillsync.Install
var confirmInstallFunc = confirmInstall

func newSkillsInstallCmd() *cobra.Command {
	var all, yes bool

	cmd := &cobra.Command{
		Use:   "install [name]...",
		Short: "Install Make platform skills",
		Example: `  makecli skills install makedsl makeui    # 按名选装
  makecli skills install --all             # 全量安装（装缺的 + 升级已有）
  makecli skills install --all -y          # 跳过确认（CI / 非交互）`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillsInstall(cmd.Context(), cmd, args, all, yes)
		},
	}

	cmd.Flags().BoolVarP(&all, "all", "a", false, "install all Make platform skills")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	return cmd
}

func runSkillsInstall(ctx context.Context, cmd *cobra.Command, names []string, all, yes bool) error {
	if all && len(names) > 0 {
		return errors.New("cannot use --all with skill names")
	}
	if !all && len(names) == 0 {
		return errors.New("specify skill names or --all (run 'makecli skills list' to see what's available)")
	}

	plan, err := planInstallFunc(ctx, names, all)
	if err != nil {
		return err
	}
	if plan.Warning != "" {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", plan.Warning)
	}

	if !yes {
		if err := confirmInstallFunc(plan); err != nil {
			return err
		}
	}

	if err := installSkillsFunc(ctx, plan); err != nil {
		return err
	}

	if len(plan.Names) > 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), skillsDoneLine(plan.Names, "installed"))
	} else {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Installed all Make platform skills")
	}
	return nil
}

// confirmInstall 在执行前确认安装计划（deploy production 同款 huh confirm 护栏）。
// 非交互终端（管道 / CI）无法确认，直接拒绝并指引 --yes，杜绝挂起。
func confirmInstall(plan skillsync.InstallPlan) error {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return errors.New("refusing to install without confirmation: re-run with --yes in a non-interactive shell")
	}

	list := "all skills"
	if len(plan.Names) > 0 {
		list = strings.Join(plan.Names, ", ")
	}

	confirmed := false
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Install Make platform skills?").
				Description(fmt.Sprintf("Source: %s\nSkills: %s\nTarget: all detected code agents",
					skillsync.SkillsSource, list)).
				Affirmative("Install").
				Negative("Abort").
				Value(&confirmed),
		),
	).Run()

	if errors.Is(err, huh.ErrUserAborted) || (err == nil && !confirmed) {
		return errors.New("install cancelled")
	}
	return err
}
