/**
 * [INPUT]: 依赖 github.com/spf13/cobra、internal/build
 * [OUTPUT]: 对外提供 newSkillsUpdateCmd 函数
 * [POS]: cmd/skills 的 update 子命令，复用 update.go 的 runSkillSync（skillsync.Sync 幂等：装缺的 + 升级已有的），与 makecli update 后置同步同一代码路径
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"github.com/qfeius/makecli/internal/build"
	"github.com/spf13/cobra"
)

func newSkillsUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "update",
		Short:        "Install missing and upgrade installed Make platform skills",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSkillSync(cmd, build.Version, false)
		},
	}
}
