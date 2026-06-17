/**
 * [INPUT]: 依赖 cmd/git（initGitRepo / ensureGitignore）、fmt、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newAppInitCmd 函数；包内 runAppInit
 * [POS]: cmd/app 的 init 子命令——把一个普通目录变成可部署的 app 仓库形态：git init（幂等）+ .gitignore 增量补齐。
 *        刻意不 commit（提交时机交还用户），也不要求 apps/dsl/app.yaml 存在（init 是通用 git 形态命令）。
 *        被 app create 末尾复用其内核（initGitRepo + ensureGitignore）后再做 initial commit；deploy 的 openRepo 门控依赖此命令先行建仓。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newAppInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init [dir]",
		Short: "Initialize git repo and .gitignore for an app (idempotent)",
		Example: `  makecli app init
  makecli app init ./shop`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := "."
			if len(args) == 1 {
				dir = args[0]
			}
			return runAppInit(dir)
		},
	}
	return cmd
}

// runAppInit 在 dir 幂等建立 git 仓库并补齐 .gitignore，逐项打印状态。
// 两步都幂等：已是仓库则跳过 init，.gitignore 已全则不改——重复运行安全。
func runAppInit(dir string) error {
	created, err := initGitRepo(dir)
	if err != nil {
		return err
	}
	if created {
		fmt.Println("git:        initialized")
	} else {
		fmt.Println("git:        already a repository")
	}

	changed, err := ensureGitignore(dir)
	if err != nil {
		return err
	}
	if changed {
		fmt.Println(".gitignore: updated")
	} else {
		fmt.Println(".gitignore: already complete")
	}
	return nil
}
