/**
 * [INPUT]: 依赖 bufio、context、fmt、io、strings；传输层来自 client.go
 * [OUTPUT]: 对外提供 RunOnce（一次性模式）与 RunREPL（交互循环）
 * [POS]: internal/agent 的会话编排——多轮历史在进程内存，流式增量直写输出；
 *        v1 纯聊天，工具与本地 session 落盘随 internal/agent 全量四模块进入
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package agent

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
)

// RunOnce 一次性模式：单条 prompt，流式打印回复。
func RunOnce(ctx context.Context, client *Client, model, systemPrompt, prompt string, output io.Writer) error {
	messages := historyWithSystem(systemPrompt)
	messages = append(messages, Message{Role: "user", Content: prompt})
	_, err := client.ChatStream(ctx, model, messages, func(delta string) {
		_, _ = fmt.Fprint(output, delta)
	})
	_, _ = fmt.Fprintln(output)
	return err
}

// RunREPL 交互循环：读一行发一轮，历史随进程存续；/exit 退出、/clear 清空历史。
func RunREPL(ctx context.Context, client *Client, model, systemPrompt string, input io.Reader, output io.Writer) error {
	_, _ = fmt.Fprintf(output, "makecli agent (model: %s) — /exit 退出, /clear 清空历史\n", model)
	history := historyWithSystem(systemPrompt)
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	for {
		_, _ = fmt.Fprint(output, "> ")
		if !scanner.Scan() {
			_, _ = fmt.Fprintln(output)
			return scanner.Err()
		}
		line := strings.TrimSpace(scanner.Text())
		switch line {
		case "":
			continue
		case "/exit", "/quit":
			return nil
		case "/clear":
			history = historyWithSystem(systemPrompt)
			_, _ = fmt.Fprintln(output, "(历史已清空)")
			continue
		}
		history = append(history, Message{Role: "user", Content: line})
		reply, err := client.ChatStream(ctx, model, history, func(delta string) {
			_, _ = fmt.Fprint(output, delta)
		})
		_, _ = fmt.Fprintln(output)
		if err != nil {
			// 单轮失败不退出循环：回滚本轮 user 消息，保住已有历史。
			history = history[:len(history)-1]
			_, _ = fmt.Fprintf(output, "error: %v\n", err)
			continue
		}
		history = append(history, Message{Role: "assistant", Content: reply})
	}
}

func historyWithSystem(systemPrompt string) []Message {
	if systemPrompt == "" {
		return nil
	}
	return []Message{{Role: "system", Content: systemPrompt}}
}
