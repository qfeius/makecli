/**
 * [INPUT]: 依赖 bufio、context、fmt、io、os、strings、internal/agent/{core,llm,loop,tool,trust}
 * [OUTPUT]: 对外提供 CodeOptions、RunCodeOnce（-p 无头模式：一轮循环到自然收尾）、
 *           RunCodeREPL（交互循环：历史进程内存续 + 副作用工具确认）
 * [POS]: internal/agent 的 code agent 编排层——把网关 Provider(llm.GatewayProvider)、
 *        七工具注册表(root=cwd)、目录信任(trust.Manager + BeforeToolCall 确认钩子)与
 *        两层循环(loop.StartRun)接成完整会话；渲染在 render.go
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package agent

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/qfeius/makecli/internal/agent/core"
	"github.com/qfeius/makecli/internal/agent/llm"
	"github.com/qfeius/makecli/internal/agent/loop"
	"github.com/qfeius/makecli/internal/agent/tool"
	"github.com/qfeius/makecli/internal/agent/trust"
)

// CodeOptions 是 code agent 会话的启动配置（cmd 层填好后透传）。
type CodeOptions struct {
	// GatewayURL 是平台 gateway 地址（LLM 走平台，设备端零厂商 key）。
	GatewayURL string
	// Token 是平台 token（Authorization: Bearer）。
	Token string
	// Model 是平台侧解析的模型别名。
	Model string
	// System 覆盖系统提示词的基座指令（--system；空则用 loop 默认基座）。
	System string
	// Approve 为 true 时启动即授予 Dir 会话信任（--approve），副作用工具免确认。
	Approve bool
	// Dir 是工作根目录（工具 root 与 AGENTS.md 链锚点）；空则取进程 cwd。
	Dir string
}

// sideEffectTools 是被目录信任门控的副作用工具（对齐 pigo cmd/pigo/trust.go）；
// 只读工具 read/grep/find/ls 永不拦。
var sideEffectTools = map[string]bool{
	"bash":  true,
	"write": true,
	"edit":  true,
}

// confirmFunc 询问一次副作用工具调用：allow=本次放行，always=同时授予本会话信任。
// nil 表示无交互通道（无头模式），未信任目录的副作用工具直接拦截。
type confirmFunc func(call core.AgentToolCall) (allow, always bool)

// codeSession 是一次 code agent 会话的活体状态：消息历史跨 prompt 增长。
type codeSession struct {
	dir      string
	agentCtx *core.AgentContext
	cfg      loop.RunConfig
}

// RunCodeOnce 无头模式（-p）：跑一轮循环到自然收尾。未信任目录的副作用工具
// 直接拦截（拦截语提示 --approve），不阻塞等待终端输入。终结失败
// （stopReason=error/aborted）作为 error 返回，由 cmd 出口转退出码 1。
func RunCodeOnce(ctx context.Context, opts CodeOptions, prompt string, output io.Writer) error {
	session, err := newCodeSession(opts, output, nil)
	if err != nil {
		return err
	}
	return session.runPrompt(ctx, prompt, output)
}

// RunCodeREPL 交互循环：读一行发一轮，历史随进程存续（内存态，不落盘）；
// /exit /quit 退出、/clear 清空历史。副作用工具在未信任目录逐次确认
// （y=允许一次 / n=拒绝 / a=本会话全允许），确认与主循环共用同一 Reader，
// 预输入不会被劈开。单轮失败打印 error 不退出循环。
func RunCodeREPL(ctx context.Context, opts CodeOptions, input io.Reader, output io.Writer) error {
	reader := bufio.NewReaderSize(input, 64<<10)
	confirm := func(call core.AgentToolCall) (bool, bool) {
		return confirmToolCall(output, reader, call)
	}
	session, err := newCodeSession(opts, output, confirm)
	if err != nil {
		return err
	}
	_, _ = fmt.Fprintf(output, "makecli agent (model: %s, cwd: %s) — /exit 退出, /clear 清空历史\n", opts.Model, session.dir)
	for {
		if ctx.Err() != nil {
			return nil
		}
		_, _ = fmt.Fprint(output, "> ")
		line, readErr := reader.ReadString('\n')
		trimmed := strings.TrimSpace(line)
		if readErr != nil && trimmed == "" {
			_, _ = fmt.Fprintln(output)
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
		switch trimmed {
		case "":
			continue
		case "/exit", "/quit":
			return nil
		case "/clear":
			session.agentCtx.Messages = nil
			_, _ = fmt.Fprintln(output, "(历史已清空)")
			continue
		default:
			if runErr := session.runPrompt(ctx, trimmed, output); runErr != nil {
				_, _ = fmt.Fprintf(output, "error: %v\n", runErr)
			}
		}
		if readErr != nil {
			// 最后一行无换行（EOF）：处理完即收。
			_, _ = fmt.Fprintln(output)
			return nil
		}
	}
}

// newCodeSession 组装一次会话：系统提示词（AGENTS.md 链，root=cwd）、七工具
// 注册表（root=cwd）、目录信任 + BeforeToolCall 钩子、指向 gateway 的 StreamFn。
func newCodeSession(opts CodeOptions, output io.Writer, confirm confirmFunc) (*codeSession, error) {
	dir := opts.Dir
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("resolve working directory: %w", err)
		}
		dir = cwd
	}

	systemPrompt, err := loop.BuildSystemPrompt(loop.PromptConfig{
		BaseInstruction: opts.System,
		WorkingDir:      dir,
		Root:            dir,
	})
	if err != nil {
		return nil, err
	}

	registry := tool.NewToolRegistry()
	for _, t := range []core.AgentTool{
		&tool.ReadTool{Root: dir},
		&tool.WriteTool{Root: dir},
		&tool.EditTool{Root: dir},
		&tool.GrepTool{Root: dir},
		&tool.FindTool{Root: dir},
		&tool.LsTool{Root: dir},
		&tool.BashTool{Dir: dir},
	} {
		if regErr := registry.Register(t); regErr != nil {
			return nil, regErr
		}
	}

	manager, err := trust.NewManager(trust.DefaultPath())
	if err != nil {
		// 坏信任存储不阻断会话：降级为进程内会话信任（不落盘），并明示用户。
		_, _ = fmt.Fprintf(output, "warning: 信任存储不可用(%v)，信任决策仅在本会话内生效\n", err)
		manager, _ = trust.NewManager("")
	}
	if opts.Approve {
		manager.SetSessionTrust(dir)
	}

	provider := llm.NewGatewayProvider(opts.GatewayURL, opts.Token, NewSessionID())
	cfg := loop.RunConfig{
		LoopConfig: loop.LoopConfig{
			Model:    opts.Model,
			APIKey:   opts.Token,
			Provider: provider.Name(),
			Stream:   llm.StreamFnFromProvider(provider),
		},
		Batch: tool.BatchConfig{ToolExecutorConfig: tool.ToolExecutorConfig{
			Registry:       registry,
			BeforeToolCall: trustBeforeToolCall(manager, dir, confirm),
		}},
		// EventBuffer 必须保持 0：确认钩子在 producer goroutine 上读写终端，
		// 依赖消费方彼时阻塞在事件接收上（对齐 pigo trust.go 的并发前提）。
	}

	return &codeSession{
		dir:      dir,
		agentCtx: &core.AgentContext{SystemPrompt: systemPrompt, Tools: registry.List()},
		cfg:      cfg,
	}, nil
}

// runPrompt 追加一条 user 消息并驱动一轮完整循环，事件经 runRenderer 行式
// 渲染到 output。返回终结失败（stopReason=error/aborted）；自然收尾返回 nil。
func (s *codeSession) runPrompt(ctx context.Context, prompt string, output io.Writer) error {
	s.agentCtx.Messages = append(s.agentCtx.Messages, core.UserMessage{
		RoleField: core.RoleUser,
		Content:   core.ContentList{core.NewTextContent(prompt)},
	})
	stream := loop.StartRun(ctx, s.agentCtx, s.cfg)
	renderer := newRunRenderer(output)
	for ev := range stream.Events() {
		renderer.handle(ev)
	}
	// finish() 总在 Close 前 SetResult，此处不会阻塞。
	if _, err := stream.Result(context.Background()); err != nil {
		return err
	}
	return renderer.runErr
}

// trustBeforeToolCall 构建门控副作用工具的 BeforeToolCall 钩子（适配自 pigo
// cmd/pigo/trust.go 的同名函数）：受信目录放行；未受信时经 confirm 询问，
// a(always) 授予会话信任；confirm 为 nil（无头）或用户拒绝则拦截，拦截语
// 作为 IsError 工具结果回喂模型。副作用工具全部声明 sequential，钩子只会在
// producer goroutine 上串行触发，无需额外互斥。
func trustBeforeToolCall(manager *trust.Manager, dir string, confirm confirmFunc) core.BeforeToolCallFunc {
	return func(ctx context.Context, call core.AgentToolCall) *core.BeforeToolCallDecision {
		if !sideEffectTools[call.Name] {
			return nil
		}
		if manager.IsTrusted(dir) {
			return nil
		}
		if confirm == nil {
			return blockDecision(fmt.Sprintf(
				"tool %q blocked: directory %s is not trusted (re-run with --approve to grant session trust)",
				call.Name, dir))
		}
		allow, always := confirm(call)
		if always {
			manager.SetSessionTrust(dir)
		}
		if !allow {
			return blockDecision(fmt.Sprintf(
				"tool %q blocked: the user denied this call in %s", call.Name, dir))
		}
		return nil
	}
}

// blockDecision 构造带说明文本的拦截决定。
func blockDecision(msg string) *core.BeforeToolCallDecision {
	return &core.BeforeToolCallDecision{
		Block:   true,
		Content: &core.ContentList{core.NewTextContent(msg)},
	}
}

// confirmToolCall 在 REPL 里询问一次副作用工具调用（适配自 pigo 的同名函数）：
// y=允许一次 / a=允许并授予本会话信任 / 其余(含空行与 EOF)=拒绝。
func confirmToolCall(output io.Writer, input *bufio.Reader, call core.AgentToolCall) (allow, always bool) {
	_, _ = fmt.Fprintf(output, "\nmakecli agent 请求在未信任目录执行 %q\n", call.Name)
	if summary := toolCallSummary(call.Name, call.Arguments); summary != "" {
		_, _ = fmt.Fprintf(output, "  %s\n", summary)
	}
	_, _ = fmt.Fprint(output, "允许? [y]=允许一次 / [n]=拒绝 / [a]=本会话全允许: ")
	line, _ := input.ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		return true, false
	case "a", "always":
		return true, true
	default:
		return false, false
	}
}
