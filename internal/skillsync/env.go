/**
 * [INPUT]: 依赖 errors、os/exec
 * [OUTPUT]: 对外提供 EnsureNpx；lookPathFunc 为测试接缝
 * [POS]: internal/skillsync 的环境门禁层，Sync / Remove / PlanInstall 在 shell out npx 前统一调用
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package skillsync

import (
	"errors"
	"os/exec"
)

// lookPathFunc 是 PATH 查找接缝，测试注入以模拟 npx 缺失。
var lookPathFunc = exec.LookPath

// EnsureNpx 确认 npx 可用。Make platform skills 经 'skills' npm CLI 分发，
// 缺 npx 时 exec 的报错晦涩、失败信息里的 manual fix 命令也没法跑，
// 这里换成面向 agent 一步收敛的安装指引。
func EnsureNpx() error {
	if _, err := lookPathFunc("npx"); err != nil {
		return errors.New(`npx not found: Make platform skills are distributed via the 'skills' npm CLI
How to fix:
  macOS:  brew install node
  or install Node.js (npx ships with npm): https://nodejs.org
Then re-run the command`)
	}
	return nil
}
