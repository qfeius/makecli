/**
 * [INPUT]: 依赖 context、os/exec、strings、time
 * [OUTPUT]: 对外提供 Sync / Options / Result / SkillsCommand，执行 Make platform skills 默认同步
 * [POS]: internal/skillsync 的编排层，被 cmd/update.go 在二进制更新后调用；隔离 npx 副作用，update 每次刷新 skills
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package skillsync

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const (
	SkillsSource  = "qfeius/make-platform-skills"
	ActionSynced  = "synced"
	ActionSkipped = "skipped"
	ActionFailed  = "failed"
)

const syncTimeout = 3 * time.Minute

// Options 控制一次 skills 同步。
type Options struct {
	Version string
	Skip    bool
}

// Result 描述同步结果，供 cmd 层渲染用户可见输出。
type Result struct {
	Action  string
	Reason  string
	Source  string
	Version string
	Command []string
	Output  string
}

// CommandString 返回可直接复制执行的同步命令。
func (r Result) CommandString() string {
	return strings.Join(r.Command, " ")
}

// SkillsCommand 返回官方 Make platform skills 安装/更新命令。
func SkillsCommand() []string {
	return []string{"npx", "-y", "skills", "add", SkillsSource, "--all", "-y"}
}

var runSkillsCommand = defaultRunSkillsCommand

// Sync 同步 Make platform skills。除非显式 Skip，否则每次执行 npx 刷新。
func Sync(ctx context.Context, opts Options) (Result, error) {
	result := Result{
		Action:  ActionSynced,
		Source:  SkillsSource,
		Version: opts.Version,
		Command: SkillsCommand(),
	}

	if opts.Skip {
		result.Action = ActionSkipped
		result.Reason = "disabled by flag"
		return result, nil
	}

	runCtx := ctx
	if runCtx == nil {
		runCtx = context.Background()
	}
	runCtx, cancel := context.WithTimeout(runCtx, syncTimeout)
	defer cancel()

	output, err := runSkillsCommand(runCtx, result.Command)
	result.Output = strings.TrimSpace(output)
	if err != nil {
		result.Action = ActionFailed
		return result, fmt.Errorf("failed to sync Make platform skills: %w\nmanual fix: %s\n%s",
			err, result.CommandString(), trimOutput(result.Output))
	}

	return result, nil
}

func defaultRunSkillsCommand(ctx context.Context, command []string) (string, error) {
	if len(command) == 0 {
		return "", fmt.Errorf("empty command")
	}

	cmd := exec.CommandContext(ctx, command[0], command[1:]...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

func trimOutput(output string) string {
	const maxOutput = 4000
	if len(output) <= maxOutput {
		return output
	}
	return output[:maxOutput] + "\n... output truncated ..."
}
