/**
 * [INPUT]: 依赖 bufio、context、encoding/json、fmt、os/exec、strings、time；接口契约来自 adapter.go
 * [OUTPUT]: 对外提供 ClaudeCode Backend（claude -p --output-format stream-json）与 parseClaudeLine（流事件解析，测试锚点）
 * [POS]: internal/daemon/adapter 的 claude-code 实现——逐行 stream-json 归一为统一 Message，result 事件收口 Result；
 *        --resume 兑现会话连续性
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// ClaudeCode 通过 Claude Code CLI 执行。
type ClaudeCode struct {
	// ExecutablePath 可覆盖二进制路径，空则 PATH 查找 "claude"。
	ExecutablePath string
}

// Provider 实现 Backend。
func (b *ClaudeCode) Provider() string { return "claude-code" }

func (b *ClaudeCode) executable() string {
	if b.ExecutablePath != "" {
		return b.ExecutablePath
	}
	return "claude"
}

// Detect 探测 CLI 可用性并取版本。
func (b *ClaudeCode) Detect(ctx context.Context) (string, error) {
	path, err := exec.LookPath(b.executable())
	if err != nil {
		return "", fmt.Errorf("claude CLI 不可用: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("claude --version: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Execute 启动 `claude -p <prompt> --output-format stream-json`。
// prompt 走 argv（无交互 stdin 协议）；权限走 bypassPermissions——设备是
// 租户自有信任域（agent-design/Design.md §6），平台只做 schema 与租约校验。
func (b *ClaudeCode) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	path, err := exec.LookPath(b.executable())
	if err != nil {
		return nil, fmt.Errorf("claude CLI 不可用: %w", err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	if opts.MaxRunDuration > 0 {
		runCtx, cancel = context.WithTimeout(ctx, opts.MaxRunDuration)
	}

	args := []string{
		"-p", prompt,
		"--output-format", "stream-json",
		"--verbose",
		"--permission-mode", "bypassPermissions",
	}
	if opts.ResumeSessionID != "" {
		args = append(args, "--resume", opts.ResumeSessionID)
	}

	cmd := exec.CommandContext(runCtx, path, args...)
	cmd.WaitDelay = 10 * time.Second
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("claude stdout pipe: %w", err)
	}
	var stderrTail strings.Builder
	cmd.Stderr = boundedWriter{&stderrTail, 4096}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start claude: %w", err)
	}

	messages := make(chan Message, 64)
	results := make(chan Result, 1)
	go func() {
		defer cancel()
		defer close(messages)
		defer close(results)

		// 取消时关掉 stdout，令 scanner 解除阻塞。
		go func() {
			<-runCtx.Done()
			_ = stdout.Close()
		}()

		var final Result
		sawResult := false
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 0, 1<<20), 10<<20)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			parsed, result, ok := parseClaudeLine(line)
			if !ok {
				continue
			}
			if result != nil {
				final = *result
				sawResult = true
				continue
			}
			for _, message := range parsed {
				select {
				case messages <- message:
				case <-runCtx.Done():
				}
			}
		}
		waitErr := cmd.Wait()
		switch {
		case sawResult && !final.IsError:
			results <- final
		case sawResult:
			results <- final
		default:
			reason := "claude 未产出 result 事件"
			if waitErr != nil {
				reason = fmt.Sprintf("claude 退出异常: %v", waitErr)
			}
			if runCtx.Err() == context.DeadlineExceeded {
				reason = "max-run-duration 超时"
			}
			if tail := strings.TrimSpace(stderrTail.String()); tail != "" {
				reason += "; stderr: " + tail
			}
			results <- Result{IsError: true, ErrorMessage: reason}
		}
	}()
	return &Session{Messages: messages, Result: results}, nil
}

// claudeStreamLine 是 stream-json 单行的形状（只取所需字段）。
type claudeStreamLine struct {
	Type      string `json:"type"`
	Subtype   string `json:"subtype,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Message   *struct {
		Content []claudeContentBlock `json:"content"`
	} `json:"message,omitempty"`

	// result 事件字段
	ResultText string `json:"result,omitempty"`
	IsError    bool   `json:"is_error,omitempty"`
	Usage      *struct {
		InputTokens              int64 `json:"input_tokens"`
		OutputTokens             int64 `json:"output_tokens"`
		CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	} `json:"usage,omitempty"`
}

type claudeContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

// parseClaudeLine 把一行 stream-json 归一为统一 Message（或最终 Result）。
// 未知/不完整行返回 ok=false 静默跳过——CLI 版本演进不应打断执行。
func parseClaudeLine(line string) ([]Message, *Result, bool) {
	var parsed claudeStreamLine
	if err := json.Unmarshal([]byte(line), &parsed); err != nil {
		return nil, nil, false
	}
	switch parsed.Type {
	case "assistant":
		if parsed.Message == nil {
			return nil, nil, false
		}
		var messages []Message
		for _, block := range parsed.Message.Content {
			switch block.Type {
			case "text":
				if block.Text != "" {
					messages = append(messages, Message{Type: MessageText, Text: block.Text})
				}
			case "thinking":
				if block.Thinking != "" {
					messages = append(messages, Message{Type: MessageThinking, Text: block.Thinking})
				}
			case "tool_use":
				messages = append(messages, Message{Type: MessageToolUse, Tool: block.Name, CallID: block.ID, Input: block.Input})
			}
		}
		return messages, nil, true
	case "user":
		if parsed.Message == nil {
			return nil, nil, false
		}
		var messages []Message
		for _, block := range parsed.Message.Content {
			if block.Type == "tool_result" {
				messages = append(messages, Message{
					Type: MessageToolResult, CallID: block.ToolUseID,
					Output: flattenToolResultContent(block.Content), IsError: block.IsError,
				})
			}
		}
		return messages, nil, true
	case "result":
		result := &Result{
			Text:         parsed.ResultText,
			IsError:      parsed.IsError,
			CLISessionID: parsed.SessionID,
		}
		if parsed.IsError {
			result.ErrorMessage = parsed.ResultText
			if result.ErrorMessage == "" {
				result.ErrorMessage = "claude result 标记失败(subtype=" + parsed.Subtype + ")"
			}
		}
		if parsed.Usage != nil {
			result.Usage = &TokenUsage{
				InputTokens:         parsed.Usage.InputTokens,
				OutputTokens:        parsed.Usage.OutputTokens,
				CacheReadTokens:     parsed.Usage.CacheReadInputTokens,
				CacheCreationTokens: parsed.Usage.CacheCreationInputTokens,
			}
		}
		return nil, result, true
	case "system":
		return nil, nil, true // 启动横幅等，忽略
	}
	return nil, nil, false
}

// flattenToolResultContent 把 tool_result 的 content（字符串或块数组）压平为文本。
func flattenToolResultContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString
	}
	var asBlocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &asBlocks); err == nil {
		var parts []string
		for _, block := range asBlocks {
			if block.Text != "" {
				parts = append(parts, block.Text)
			}
		}
		return strings.Join(parts, "\n")
	}
	return string(raw)
}

// boundedWriter 只保留前 limit 字节——stderr 尾部诊断，不无限增长。
type boundedWriter struct {
	builder *strings.Builder
	limit   int
}

func (w boundedWriter) Write(p []byte) (int, error) {
	remaining := w.limit - w.builder.Len()
	if remaining > 0 {
		if len(p) > remaining {
			w.builder.Write(p[:remaining])
		} else {
			w.builder.Write(p)
		}
	}
	return len(p), nil
}
