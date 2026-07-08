/**
 * [INPUT]: 依赖 fmt、strings、github.com/spf13/cobra、internal/skillsync
 * [OUTPUT]: 对外提供 newSkillsRemoveCmd 函数
 * [POS]: cmd/skills 的 remove 子命令，透传 skillsync.Remove（来源校验挡住误删第三方 skills），名字必填
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"
	"strings"

	"github.com/qfeius/makecli/internal/skillsync"
	"github.com/spf13/cobra"
)

// removeSkillsFunc 包装 skillsync.Remove，便于测试打桩避免真实执行 npx。
var removeSkillsFunc = skillsync.Remove

func newSkillsRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "remove <name>...",
		Short:        "Remove installed Make platform skills",
		Args:         cobra.MinimumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := removeSkillsFunc(cmd.Context(), args); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Removed: %s\n", strings.Join(args, ", "))
			return nil
		},
	}
}
