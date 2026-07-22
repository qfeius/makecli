/**
 * [INPUT]: 依赖 fmt、os、path/filepath、strings；协议类型来自 protocol.go
 * [OUTPUT]: 对外提供 PrepareWorkDir（工作目录定位/创建 + instructions 渲染为 CLI 原生上下文文件）与 BuildPrompt（触发区间事件 → prompt）
 * [POS]: internal/daemon 的执行环境层——v1 最小版：目录 + 上下文文件（bare-clone 仓库缓存与 worktree 随 v2）
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// PrepareWorkDir 定位（或创建）run 的工作目录并渲染 agent instructions。
// 连续性优先：claim 下发的 work_dir 存在即沿用；否则在 baseDir 下按
// session 建目录。instructions 渲染为 CLAUDE.md 与 AGENTS.md——两个 CLI
// 的原生发现路径都覆盖，呈现按 provider 适配的差异就止步于文件名。
func PrepareWorkDir(baseDir string, claim RunClaim) (string, error) {
	workDir := claim.Resume.WorkDir
	if workDir == "" {
		workDir = filepath.Join(baseDir, claim.SessionID)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return "", fmt.Errorf("create work dir: %w", err)
	}
	if instructions := strings.TrimSpace(claim.Agent.Instructions); instructions != "" {
		content := fmt.Sprintf("# %s\n\n%s\n", claim.Agent.Name, instructions)
		for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
			if err := os.WriteFile(filepath.Join(workDir, name), []byte(content), 0o644); err != nil {
				return "", fmt.Errorf("render %s: %w", name, err)
			}
		}
	}
	return workDir, nil
}

// BuildPrompt 把触发区间的 user_message 事件拼为 prompt 文本。
// 合并语义在此兑现：claim 的 trigger 区间可能覆盖多条积压消息，一次带走。
func BuildPrompt(events []Event) string {
	var parts []string
	for _, event := range events {
		if event.Type != "user_message" {
			continue
		}
		var payload UserMessagePayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			continue
		}
		text := renderBlocksText(payload.Blocks)
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// renderBlocksText 把内容块降级为纯文本（mention 以 @name 呈现）。
func renderBlocksText(blocks []Block) string {
	var parts []string
	for _, block := range blocks {
		switch block.Kind {
		case "text":
			if block.Text != "" {
				parts = append(parts, block.Text)
			}
		case "mention":
			if block.Text != "" {
				parts = append(parts, "@"+block.Text)
			}
		case "image", "file":
			if block.URL != "" {
				parts = append(parts, block.URL)
			}
		}
	}
	return strings.TrimSpace(strings.Join(parts, " "))
}
