/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、cmd/app（loadAppManifestFromFile）、errors、fmt、os、charm.land/huh/v2（交互确认表单）、github.com/mattn/go-isatty（TTY 检测）、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newAppDeleteCmd 函数；包级 confirmDeleteFunc 可打桩变量（测试替换，参照 deploy.go gitPushFunc 模式）
 * [POS]: cmd/app 的 delete 子命令，删除前要求输入 app key 确认（gh repo delete 同款，huh 表单实现），--yes 跳过；支持 -f 文件模式
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"errors"
	"fmt"
	"os"

	"charm.land/huh/v2"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
)

// confirmDeleteFunc 为包级可打桩变量，单测替换以隔离真实终端交互
var confirmDeleteFunc = confirmDeleteByTypingKey

func newAppDeleteCmd() *cobra.Command {
	var file string
	var yes bool

	cmd := &cobra.Command{
		Use:   "delete [key]",
		Short: "Delete an app on Make",
		Example: `  makecli app delete myapp
  makecli app delete myapp --yes
  makecli app delete -f app.yaml`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if file != "" {
				return runAppDeleteFromFile(file, yes)
			}
			if len(args) == 0 {
				return fmt.Errorf("requires app key or -f flag")
			}
			return runAppDelete(args[0], yes)
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "path to YAML file containing Make.App resource")
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the deletion confirmation prompt")
	return cmd
}

func runAppDeleteFromFile(path string, skipConfirm bool) error {
	manifest, err := loadAppManifestFromFile(path)
	if err != nil {
		return err
	}
	return runAppDelete(manifest.Key, skipConfirm)
}

func runAppDelete(key string, skipConfirm bool) error {
	if !skipConfirm {
		if err := confirmDeleteFunc(key); err != nil {
			return err
		}
	}

	client, err := newClientFromProfile()
	if err != nil {
		return err
	}

	if err := client.DeleteApp(key); err != nil {
		return err
	}

	fmt.Printf("App '%s' deleted successfully\n", key)
	return nil
}

// confirmDeleteByTypingKey 要求用户原样输入 app key 才放行删除（gh repo delete 同款强护栏）。
// 非交互终端（管道 / CI）无法输入确认，直接拒绝并指引 --yes，杜绝挂起。
// huh 表单的 Validate 在输入 ≠ key 时阻断提交，唯一出路是输对 key 或 Ctrl-C 取消。
func confirmDeleteByTypingKey(key string) error {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return fmt.Errorf("refusing to delete %q without confirmation: re-run with --yes in a non-interactive shell", key)
	}

	var typed string
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(fmt.Sprintf("Delete app %q — this cannot be undone.", key)).
				Description(fmt.Sprintf("Type %q to confirm:", key)).
				Value(&typed).
				Validate(func(s string) error {
					if s != key {
						return fmt.Errorf("does not match %q", key)
					}
					return nil
				}),
		),
	).Run()

	if errors.Is(err, huh.ErrUserAborted) {
		return fmt.Errorf("deletion of %q cancelled", key)
	}
	return err
}
