/**
 * [INPUT]: 依赖 encoding/json、errors、fmt、io、os、strings、github.com/mattn/go-isatty、internal/agent/core
 * [OUTPUT]: 包内提供 runRenderer（loop 事件流的行式渲染）与 toolCallSummary（工具调用一行摘要，确认提示复用）
 * [POS]: internal/agent 的 code agent 渲染层（无 TUI）：assistant 文本增量直写、
 *        工具调用打 `⚙ name: 摘要` 行、工具结果打截断预览（IsError 红色 ✗ 前缀）、turn 边界空行；
 *        终结失败只记录不打印（打印责任在调用方：-p 走 error 返回、REPL 打印后续行）
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package agent

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/qfeius/makecli/internal/agent/core"
)

// ANSI 片段：仅在输出为终端时启用（管道/测试 buffer 保持纯文本）。
const (
	ansiRed   = "\x1b[31m"
	ansiReset = "\x1b[0m"
)

// previewMaxLines / previewMaxChars 约束工具结果预览的体量。
const (
	previewMaxLines = 3
	previewMaxChars = 200
)

// summaryMaxChars 约束 ⚙ 行与确认提示里的一行摘要长度。
const summaryMaxChars = 120

// runRenderer consumes a run's AgentEvents and renders them line-based:
// streamed assistant text verbatim, one ⚙ line per tool call, a truncated
// preview per tool result, and a blank line at each turn boundary. 终结失败
// （stopReason=error/aborted）记入 runErr 由调用方呈现。
type runRenderer struct {
	out     io.Writer
	color   bool
	printed int  // 当前 assistant 消息已打印的文本长度（增量流式的水位）
	atLine  bool // 光标是否位于行首
	runErr  error
}

// newRunRenderer builds a renderer for one run.
func newRunRenderer(out io.Writer) *runRenderer {
	color := false
	if f, ok := out.(*os.File); ok {
		color = isatty.IsTerminal(f.Fd())
	}
	return &runRenderer{out: out, color: color, atLine: true}
}

// handle dispatches one loop event to the matching renderer.
func (r *runRenderer) handle(ev core.AgentEvent) {
	switch e := ev.(type) {
	case core.MessageStartEvent:
		r.printed = 0
		r.streamText(e.Message)
	case core.MessageUpdateEvent:
		r.streamText(e.Message)
	case core.MessageEndEvent:
		r.streamText(e.Message)
		r.ensureNewline()
	case core.ToolExecutionStartEvent:
		r.ensureNewline()
		raw, _ := e.Args.(json.RawMessage)
		_, _ = fmt.Fprintf(r.out, "⚙ %s: %s\n", e.ToolName, toolCallSummary(e.ToolName, raw))
	case core.ToolExecutionEndEvent:
		r.ensureNewline()
		r.renderToolResult(e)
	case core.TurnEndEvent:
		r.ensureNewline()
		_, _ = fmt.Fprintln(r.out)
		r.recordFailure(e.Message)
	}
}

// streamText prints the not-yet-printed suffix of an assistant partial, so the
// consumer sees text as it streams（事件带全量 partial，水位差即增量）。
func (r *runRenderer) streamText(m core.AgentMessage) {
	assistant, ok := m.(core.AssistantMessage)
	if !ok {
		return
	}
	text := core.ContentToText(assistant.Content)
	if len(text) <= r.printed {
		return
	}
	delta := text[r.printed:]
	r.printed = len(text)
	_, _ = fmt.Fprint(r.out, delta)
	r.atLine = strings.HasSuffix(delta, "\n")
}

// ensureNewline moves the cursor to a fresh line after streamed text.
func (r *runRenderer) ensureNewline() {
	if !r.atLine {
		_, _ = fmt.Fprintln(r.out)
		r.atLine = true
	}
}

// renderToolResult prints a truncated preview of one tool result: up to
// previewMaxLines lines / previewMaxChars chars, two-space indented; an error
// result is prefixed ✗ (red on a terminal).
func (r *runRenderer) renderToolResult(e core.ToolExecutionEndEvent) {
	text := strings.TrimSpace(core.ContentToText(e.Result.Content))
	if e.IsError {
		prefix := "✗"
		if r.color {
			prefix = ansiRed + "✗" + ansiReset
		}
		if text == "" {
			text = e.ToolName + " failed"
		}
		for i, line := range previewLines(text) {
			if i == 0 {
				_, _ = fmt.Fprintf(r.out, "  %s %s\n", prefix, line)
				continue
			}
			_, _ = fmt.Fprintf(r.out, "    %s\n", line)
		}
		return
	}
	for _, line := range previewLines(text) {
		_, _ = fmt.Fprintf(r.out, "  %s\n", line)
	}
}

// recordFailure captures a terminal failure message so the caller can surface
// it（-p 转退出码 1；REPL 打 error 行）。TurnEnd 上采集可同时覆盖「建流即败被
// 合成终结消息」与「流中败」两条路径。
func (r *runRenderer) recordFailure(msg core.AssistantMessage) {
	if r.runErr != nil {
		return
	}
	switch msg.StopReason {
	case core.StopReasonError, core.StopReasonAborted:
		reason := msg.ErrorMessage
		if reason == "" {
			reason = msg.StopReason
		}
		r.runErr = errors.New(reason)
	}
}

// previewLines splits text into at most previewMaxLines lines under a total
// previewMaxChars budget, appending … when truncated.
func previewLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	truncated := false
	if len(lines) > previewMaxLines {
		lines = lines[:previewMaxLines]
		truncated = true
	}
	budget := previewMaxChars
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		runes := []rune(line)
		if len(runes) > budget {
			line = string(runes[:budget])
			truncated = true
			budget = 0
		} else {
			budget -= len(runes)
		}
		out = append(out, line)
		if budget <= 0 {
			break
		}
	}
	if truncated && len(out) > 0 {
		out[len(out)-1] += " …"
	}
	return out
}

// toolCallSummary renders a one-line preview of a tool call (⚙ 行与确认提示
// 共用)：bash 取 command，文件工具取 path，搜索工具取 pattern，解不出参数时
// 回退截断后的原始 JSON。适配自 pigo cmd/pigo/trust.go 的 toolCallSummary。
func toolCallSummary(name string, args json.RawMessage) string {
	raw := strings.TrimSpace(string(args))
	if raw == "" || raw == "{}" {
		return ""
	}
	var fields map[string]any
	if err := json.Unmarshal(args, &fields); err != nil {
		return truncateOneLine(raw)
	}
	str := func(key string) string {
		s, _ := fields[key].(string)
		return s
	}
	switch name {
	case "bash":
		if cmd := str("command"); cmd != "" {
			return truncateOneLine(cmd)
		}
	case "read", "write", "edit", "ls":
		if p := str("path"); p != "" {
			return truncateOneLine(p)
		}
	case "grep", "find":
		if pat := str("pattern"); pat != "" {
			if p := str("path"); p != "" {
				return truncateOneLine(pat + " " + p)
			}
			return truncateOneLine(pat)
		}
	}
	return truncateOneLine(raw)
}

// truncateOneLine collapses s to a single line capped at summaryMaxChars runes.
func truncateOneLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i]) + " …"
	}
	runes := []rune(s)
	if len(runes) > summaryMaxChars {
		s = string(runes[:summaryMaxChars]) + " …"
	}
	return s
}
