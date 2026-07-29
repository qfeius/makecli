/**
 * [INPUT]: 依赖 context、encoding/json、fmt、io、strings、sync、internal/agent/{core,tool}
 * [OUTPUT]: 包内提供 bangCommand（识别 `!cmd` 直通行）、runBangCommand（本机 shell 执行 +
 *           流式渲染 + 产出回喂历史的转录）、bangHint（裸 `!` 的用法提示）
 * [POS]: internal/agent 的本地命令直通层（对齐 Claude Code 的 `!cmd`）——REPL 里以 `!`
 *        开头的一行不发起任何 LLM 请求，直接跑本机 shell（复用 tool.BashTool 的超时/
 *        退出码/取消语义），输出流式直写终端并作为上下文进对话历史
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/qfeius/makecli/internal/agent/core"
	"github.com/qfeius/makecli/internal/agent/tool"
)

// bangPrefix 是本地命令直通前缀。
const bangPrefix = "!"

// bangHistoryMaxChars 约束进对话历史的输出体量：截头保留（与 previewLines 同向），
// 免得一条 `!cat 大文件` 撑爆上下文。
const bangHistoryMaxChars = 16 << 10

// bangCallID 是喂给 BashTool 的固定调用 id——本地直通不属于任何 LLM 工具调用。
const bangCallID = "local-bang"

// bangHint 是裸 `!` 的用法提示。
const bangHint = "用法: !<命令>，例如 !ls -la（在本机执行，不发起 LLM 请求）"

// bangCommand 识别本地命令直通行：`!ls` 与 `! ls` 都取到 ls，裸 `!` 取到空串
// （调用方打 bangHint）。第二个返回值表示这一行是否是直通行。
func bangCommand(line string) (string, bool) {
	if !strings.HasPrefix(line, bangPrefix) {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(line, bangPrefix)), true
}

// runBangCommand 在 dir（空=进程 cwd）跑一条本机 shell 命令，输出流式直写
// output，返回值是喂回对话历史的转录文本。
//
// 刻意不过目录信任门控：门控（trustBeforeToolCall）防的是模型自作主张调副作用
// 工具，而这条命令是用户在提示符下亲手敲的——用户对自己的机器无需向自己申请授权。
func runBangCommand(ctx context.Context, dir, command string, output io.Writer) string {
	_, _ = fmt.Fprintf(output, "$ %s\n", command)

	// BashTool 的 onUpdate 带全量快照，水位差即增量；stdout/stderr 合流可能在
	// 两个 goroutine 上并发触发，故水位自带互斥。
	var (
		mu      sync.Mutex
		printed int
		atLine  = true
	)
	onUpdate := func(res core.AgentToolResult) {
		text := core.ContentToText(res.Content)
		mu.Lock()
		defer mu.Unlock()
		if len(text) <= printed {
			return
		}
		delta := text[printed:]
		printed = len(text)
		_, _ = io.WriteString(output, delta)
		atLine = strings.HasSuffix(delta, "\n")
	}

	args, _ := json.Marshal(map[string]string{"command": command})
	result, err := (&tool.BashTool{Dir: dir}).Execute(ctx, bangCallID, args, onUpdate)

	mu.Lock()
	if !atLine {
		_, _ = fmt.Fprintln(output)
	}
	mu.Unlock()

	transcript := core.ContentToText(result.Content)
	if err != nil {
		// BashTool 的错误消息把完整输出附在首行之后；输出已流式打过，只补状态行。
		status := bangStatus(err)
		_, _ = fmt.Fprintf(output, "  %s %s\n", errorMark(output), status)
		transcript = strings.TrimRight(transcript, "\n")
		if transcript != "" {
			transcript += "\n"
		}
		transcript += status
	}
	return bangHistoryEntry(command, transcript)
}

// bangStatus 取错误消息首行（BashTool 在首行之后附完整输出，此处不重复呈现）。
func bangStatus(err error) string {
	msg := err.Error()
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	return msg
}

// bangHistoryEntry 把一次本地执行折成喂回模型的一段文本：明说这是用户的本机命令
// 转录（是上下文不是提问），命令与输出照 shell 惯例排版。系统提示词是英文，
// 此框架句一并用英文。
func bangHistoryEntry(command, transcript string) string {
	body := strings.TrimRight(transcript, "\n")
	if runes := []rune(body); len(runes) > bangHistoryMaxChars {
		body = string(runes[:bangHistoryMaxChars]) + "\n…(output truncated)"
	}
	if body == "" {
		body = "(no output)"
	}
	return fmt.Sprintf(
		"The user ran a shell command locally. The transcript below is context for the conversation, not a request.\n\n$ %s\n%s",
		command, body)
}
