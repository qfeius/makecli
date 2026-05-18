/**
 * [INPUT]: 依赖 github.com/spf13/cobra、internal/update、internal/build
 * [OUTPUT]: 对外提供 newUpdateCmd 函数
 * [POS]: cmd 模块的 update 子命令，从 GitHub Releases 自更新二进制；
 *        无 arg 走 latest 流程，有 arg 走指定版本流程；降级需 --force；
 *        DEV 版本跳过比较
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"
	"strings"

	"github.com/qfeius/makecli/internal/build"
	"github.com/qfeius/makecli/internal/update"
	"github.com/spf13/cobra"
)

// applyFunc 包装 update.Apply，便于测试打桩避免真实替换二进制。
var applyFunc = update.Apply

func newUpdateCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "update [version]",
		Short: "Update makecli to the latest or a specific version",
		Example: `  makecli update
  makecli update v0.2.0
  makecli update 0.2.0
  makecli update --force v0.0.1`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			target := ""
			if len(args) == 1 {
				target = args[0]
			}
			return runUpdate(cmd, target, force)
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "allow downgrade to an older version")
	return cmd
}

func runUpdate(cmd *cobra.Command, target string, force bool) error {
	currentVersion := build.Version
	if target == "" {
		return runUpdateLatest(cmd, currentVersion)
	}
	return runUpdateSpecific(cmd, currentVersion, target, force)
}

func runUpdateLatest(cmd *cobra.Command, currentVersion string) error {
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Checking for updates...\n")

	release, newer, err := update.CheckLatest(currentVersion)
	if err != nil {
		return err
	}

	if !newer {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Already up to date (%s)\n", release.TagName)
		return nil
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Updating makecli: %s → %s\n",
		formatCurrentVersion(currentVersion), release.TagName)

	if err := applyFunc(release); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Updated makecli: %s → %s\n",
		formatCurrentVersion(currentVersion), release.TagName)
	return nil
}

func runUpdateSpecific(cmd *cobra.Command, currentVersion, target string, force bool) error {
	tag, err := update.NormalizeTag(target)
	if err != nil {
		return err
	}

	release, err := update.GetRelease(tag)
	if err != nil {
		return err
	}

	cmp := update.CompareVersions(tag, currentVersion)
	switch {
	case cmp == 0:
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Already at %s, skipping.\n", tag)
		return nil
	case cmp < 0 && !force:
		return fmt.Errorf("%s is older than current %s. Use --force to downgrade",
			tag, formatCurrentVersion(currentVersion))
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Updating makecli: %s → %s\n",
		formatCurrentVersion(currentVersion), tag)

	if err := applyFunc(release); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Updated makecli: %s → %s\n",
		formatCurrentVersion(currentVersion), tag)
	return nil
}

// formatCurrentVersion 格式化当前版本号用于显示
func formatCurrentVersion(v string) string {
	v = strings.TrimPrefix(v, "v")
	if v == "DEV" {
		return v
	}
	return "v" + v
}
