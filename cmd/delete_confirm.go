/**
 * [INPUT]: 依赖 errors、fmt、os、charm.land/huh/v2、github.com/mattn/go-isatty
 * [OUTPUT]: 包内 confirmDeletionByTypingKey，共用删除确认表单
 * [POS]: cmd 的删除交互原语，由 app delete 与 agent delete 传入 key 和目标描述
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package cmd

import (
	"errors"
	"fmt"
	"os"

	"charm.land/huh/v2"
	"github.com/mattn/go-isatty"
)

// confirmDeletionByTypingKey 要求原样输入 key；非交互终端拒绝并指引 --yes。
func confirmDeletionByTypingKey(key, target string) error {
	if !isatty.IsTerminal(os.Stdin.Fd()) {
		return fmt.Errorf("refusing to delete %s without confirmation: re-run with --yes in a non-interactive shell", target)
	}
	var typed string
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title(fmt.Sprintf("Delete %s — this cannot be undone.", target)).
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
		return fmt.Errorf("deletion of %s cancelled", target)
	}
	return err
}
