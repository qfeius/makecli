/**
 * [INPUT]: 依赖 github.com/spf13/cobra、internal/build
 * [OUTPUT]: 对外提供 newSkillsUpdateCmd 函数
 * [POS]: cmd/skills 的 update 子命令，与 makecli update 共用 runSkillSync，按 settings.channel 覆盖安装同名 Skill
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package cmd

import (
	"github.com/qfeius/makecli/internal/build"
	"github.com/spf13/cobra"
)

func newSkillsUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update",
		Short: "Install missing and upgrade installed Make platform skills",
		Long: "Install missing and overwrite installed Make platform skills from the configured channel.\n" +
			"[settings] channel selects main (stable, default) or beta.\n" +
			"Switch channels with 'makecli settings set channel stable|beta', then run this command.\n" +
			"Skills absent from the selected branch are kept; uninstall them explicitly if needed.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillSync(cmd, build.Version, false)
		},
	}
}
