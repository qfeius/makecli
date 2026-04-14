/**
 * [INPUT]: 依赖 fmt、os、path/filepath、github.com/spf13/cobra、github.com/qfeius/makecli/agents
 * [OUTPUT]: 对外提供 newAppInitCmd 函数
 * [POS]: cmd/app 的 init 子命令，在目标目录创建 CLAUDE.md 和 AGENTS.md（内容来自 embed）
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/qfeius/makecli/agents"
	"github.com/spf13/cobra"
)

// initFiles app init 需要创建的文件列表
var initFiles = []string{"CLAUDE.md", "AGENTS.md"}

func newAppInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:          "init [folder]",
		Short:        "Initialize an app with CLAUDE.md and AGENTS.md",
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			folder := "."
			if len(args) > 0 {
				folder = args[0]
			}
			return runAppInit(folder)
		},
	}
}

func runAppInit(folder string) error {
	// 目录不存在则创建
	if err := os.MkdirAll(folder, 0755); err != nil {
		return fmt.Errorf("failed to create '%s': %w", folder, err)
	}

	// 任一配置文件已存在则拒绝
	for _, name := range initFiles {
		target := filepath.Join(folder, name)
		if _, err := os.Stat(target); err == nil {
			return fmt.Errorf("'%s' already exists", target)
		}
	}

	// 从 embed.FS 读取模板并写出
	for _, name := range initFiles {
		data, err := agents.Templates.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(folder, name), data, 0644); err != nil {
			return err
		}
	}

	fmt.Printf("Initialized '%s' with CLAUDE.md and AGENTS.md\n", folder)
	return nil
}
