/**
 * [INPUT]: 依赖 fmt、os、path/filepath、strings；协议类型来自 protocol.go
 * [OUTPUT]: 对外提供 PrepareWorkDir（工作目录定位/创建 + description 身份职责与 instructions 执行要求渲染为 CLI 原生上下文文件）与 BuildPrompt（触发区间事件 → prompt）
 * [POS]: internal/daemon 的执行环境层——v1 最小版：目录 + 上下文文件（bare-clone 仓库缓存与 worktree 随 v2）
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// PrepareWorkDir 定位（或创建）run 的工作目录并渲染 agent 身份与指令。
// 连续性优先：claim 下发的 workDir 可用即沿用；不可用（跨设备/跨 OS 的
// 遗留路径）则回退 baseDir 下按 session 建目录并报告 resumable=false——
// 调用方须同时放弃 cliSessionID（CLI 会话是设备本地状态，目录都不在，
// 会话必然也不在）。description 与 instructions 分栏渲染为 CLAUDE.md 与
// AGENTS.md——两个 CLI 的原生发现路径都覆盖，呈现按 provider 适配的差异
// 就止步于文件名。
func PrepareWorkDir(baseDir string, claim RunClaim) (workDir string, resumable bool, err error) {
	resumable = true
	workDir = claim.Resume.WorkDir
	if workDir == "" || os.MkdirAll(workDir, 0o755) != nil {
		resumable = workDir == "" // 显式回退：resume 目录建不出来即放弃连续性
		workDir = filepath.Join(baseDir, claim.SessionID)
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return "", false, fmt.Errorf("create work dir: %w", err)
	}
	description := strings.TrimSpace(claim.Agent.Description)
	instructions := strings.TrimSpace(claim.Agent.Instructions)
	if description != "" || instructions != "" {
		var content strings.Builder
		fmt.Fprintf(&content, "# %s\n", claim.Agent.Name)
		if description != "" {
			fmt.Fprintf(&content, "\n## 身份与职责\n\n%s\n", description)
		}
		if instructions != "" {
			fmt.Fprintf(&content, "\n## 执行要求\n\n%s\n", instructions)
		}
		for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
			if err := os.WriteFile(filepath.Join(workDir, name), []byte(content.String()), 0o644); err != nil {
				return "", false, fmt.Errorf("render %s: %w", name, err)
			}
		}
	}
	return workDir, resumable, nil
}

// skillRootNames 是各 CLI 的原生技能发现路径（Design.md §8.1「skills 写入各
// CLI 原生发现路径」）。两个都写：呈现按 provider 适配的差异就止步于目录名，
// 与 CLAUDE.md/AGENTS.md 双写同一裁决。
var skillRootNames = []string{
	filepath.Join(".claude", "skills"),
	filepath.Join(".agents", "skills"),
}

// BlobFetcher 由 daemon 的 Client 实现，按 ref 取附件内容。
type BlobFetcher interface {
	FetchBlob(ctx context.Context, ref string, limit int64) ([]byte, error)
}

// maximumSkillFileBytes 与服务端契约上限一致（Contract.md §9.2）。
const maximumSkillFileBytes = 1 << 20

// RenderSkills 把 claim 下发的技能快照物化进工作目录。
//
// **整组重写**：先清空技能根目录再写。workDir 跨 run 复用（连续性优先），
// 增量补写会让上一轮解绑的技能阴魂不散——CLI 仍会发现它、仍会照着它行事，
// 而管理台上它已经不在了。清空是唯一能让"解绑"真正生效的做法。
//
// 附件回源失败不打断 run：正文仍然写下去，缺的文件在返回的 warnings 里，
// 由调用方决定怎么呈现。
func RenderSkills(ctx context.Context, workDir string, skills []SkillBundle, fetcher BlobFetcher) (warnings []string, err error) {
	for _, root := range skillRootNames {
		absoluteRoot := filepath.Join(workDir, root)
		// 先清后写：解绑的技能必须从磁盘上消失。
		if err := os.RemoveAll(absoluteRoot); err != nil {
			return nil, fmt.Errorf("clear skill root %s: %w", absoluteRoot, err)
		}
		if len(skills) == 0 {
			continue
		}
		if err := os.MkdirAll(absoluteRoot, 0o755); err != nil {
			return nil, fmt.Errorf("create skill root %s: %w", absoluteRoot, err)
		}
	}
	if len(skills) == 0 {
		return nil, nil
	}

	for i := range skills {
		skill := &skills[i]
		if !safeSkillSegment(skill.Name) {
			warnings = append(warnings, "技能名形状非法，已跳过: "+skill.Name)
			continue
		}
		// 附件只回源一次，两个根目录共用同一份内容——省掉一半网络往返。
		contents := make(map[string][]byte, len(skill.Files))
		for j := range skill.Files {
			file := &skill.Files[j]
			if !safeSkillRelativePath(file.Path) {
				warnings = append(warnings, skill.Name+" 的附件路径形状非法，已跳过: "+file.Path)
				continue
			}
			content, fetchErr := fetcher.FetchBlob(ctx, file.BlobRef, maximumSkillFileBytes)
			if fetchErr != nil {
				warnings = append(warnings, skill.Name+" 的附件回源失败: "+file.Path)
				continue
			}
			contents[file.Path] = content
		}
		for _, root := range skillRootNames {
			directory := filepath.Join(workDir, root, skill.Name)
			if err := os.MkdirAll(directory, 0o755); err != nil {
				return warnings, fmt.Errorf("create skill dir %s: %w", directory, err)
			}
			if err := os.WriteFile(filepath.Join(directory, "SKILL.md"),
				[]byte(renderSkillMarkdown(skill)), 0o644); err != nil {
				return warnings, fmt.Errorf("render SKILL.md for %s: %w", skill.Name, err)
			}
			for relative, content := range contents {
				target := filepath.Join(directory, filepath.FromSlash(relative))
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					return warnings, fmt.Errorf("create skill file dir %s: %w", target, err)
				}
				if err := os.WriteFile(target, content, 0o644); err != nil {
					return warnings, fmt.Errorf("write skill file %s: %w", target, err)
				}
			}
		}
	}
	return warnings, nil
}

// renderSkillMarkdown 产出带 frontmatter 的 SKILL.md——name/description 是各
// CLI 渐进披露的索引层，正文才是按需读取的第二层。
func renderSkillMarkdown(skill *SkillBundle) string {
	var content strings.Builder
	content.WriteString("---\n")
	content.WriteString("name: ")
	content.WriteString(skill.Name)
	content.WriteString("\n")
	if description := strings.TrimSpace(skill.Description); description != "" {
		content.WriteString("description: ")
		// frontmatter 是单行标量：换行会把文档结构撑破，压平成空格。
		content.WriteString(strings.Join(strings.Fields(description), " "))
		content.WriteString("\n")
	}
	content.WriteString("---\n\n")
	content.WriteString(skill.Body)
	if !strings.HasSuffix(skill.Body, "\n") {
		content.WriteString("\n")
	}
	return content.String()
}

// safeSkillSegment 校验单个目录名（技能名）。
func safeSkillSegment(candidate string) bool {
	if candidate == "" || candidate == "." || candidate == ".." {
		return false
	}
	return !strings.ContainsAny(candidate, "/\\\x00")
}

// safeSkillRelativePath 校验附件相对路径：写入侧已收口，此处贴着拼接点复核。
func safeSkillRelativePath(candidate string) bool {
	if candidate == "" || strings.ContainsAny(candidate, "\\\x00") {
		return false
	}
	if path.IsAbs(candidate) || path.Clean(candidate) != candidate {
		return false
	}
	for _, segment := range strings.Split(candidate, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
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
