/**
 * [INPUT]: 依赖 bufio、context、encoding/json、fmt、os/exec、strings、sync、time；接口契约来自 adapter.go
 * [OUTPUT]: 对外提供 Codex Backend（长驻 codex app-server 的 JSON-RPC 会话）与 codexRPC 精简客户端
 * [POS]: internal/daemon/adapter 的 codex 实现——initialize → thread/start|resume → turn/start，通知流归一为统一 Message，
 *        审批请求自动放行（设备是租户自有信任域）
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
	"sync"
	"time"
)

// Codex 通过 codex app-server（JSON-RPC over stdio）执行。
type Codex struct {
	// ExecutablePath 可覆盖二进制路径，空则 PATH 查找 "codex"。
	ExecutablePath string
}

// Provider 实现 Backend。
func (b *Codex) Provider() string { return "codex" }

func (b *Codex) executable() string {
	if b.ExecutablePath != "" {
		return b.ExecutablePath
	}
	return "codex"
}

// Detect 探测 CLI 可用性并取版本。
func (b *Codex) Detect(ctx context.Context) (string, error) {
	path, err := exec.LookPath(b.executable())
	if err != nil {
		return "", fmt.Errorf("codex CLI 不可用: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return "", fmt.Errorf("codex --version: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// Execute 启动 `codex app-server --listen stdio://` 并完成一轮 turn：
// initialize → thread/start|resume → turn/start → 等待 turn/completed。
// resume 失败（未知 thread 等协议错误）自动回退 thread/start——会话连续性
// 是尽力而为，正确性由平台事件流保证。
func (b *Codex) Execute(ctx context.Context, prompt string, opts ExecOptions) (*Session, error) {
	path, err := exec.LookPath(b.executable())
	if err != nil {
		return nil, fmt.Errorf("codex CLI 不可用: %w", err)
	}
	runCtx, cancel := context.WithCancel(ctx)
	if opts.MaxRunDuration > 0 {
		runCtx, cancel = context.WithTimeout(ctx, opts.MaxRunDuration)
	}

	cmd := exec.CommandContext(runCtx, path, "app-server", "--listen", "stdio://")
	cmd.WaitDelay = 10 * time.Second
	if opts.WorkDir != "" {
		cmd.Dir = opts.WorkDir
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("codex stdout pipe: %w", err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("codex stdin pipe: %w", err)
	}
	var stderrTail strings.Builder
	cmd.Stderr = boundedWriter{&stderrTail, 4096}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start codex: %w", err)
	}

	messages := make(chan Message, 64)
	results := make(chan Result, 1)
	rpc := &codexRPC{
		stdin:    stdin,
		pending:  map[int64]chan codexRPCResult{},
		turnDone: make(chan codexTurnOutcome, 1),
		messages: messages,
		runCtx:   runCtx,
	}
	go rpc.readLoop(stdout)

	go func() {
		defer cancel()
		defer close(messages)
		defer close(results)
		defer func() { _ = stdin.Close() }()

		result := b.driveTurn(runCtx, rpc, prompt, opts)
		if result.IsError && runCtx.Err() == context.DeadlineExceeded {
			result.ErrorMessage = "max-run-duration 超时"
		}
		if result.IsError {
			if tail := strings.TrimSpace(stderrTail.String()); tail != "" {
				result.ErrorMessage += "; stderr: " + tail
			}
		}
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		results <- result
	}()
	return &Session{Messages: messages, Result: results}, nil
}

// driveTurn 执行握手、线程定位与单轮 turn，返回最终结果。
func (b *Codex) driveTurn(ctx context.Context, rpc *codexRPC, prompt string, opts ExecOptions) Result {
	if _, err := rpc.request(ctx, "initialize", map[string]any{
		"clientInfo":   map[string]any{"name": "makecli-daemon", "version": "0.1.0"},
		"capabilities": map[string]any{"experimentalApi": true},
	}); err != nil {
		return Result{IsError: true, ErrorMessage: "codex initialize 失败: " + err.Error()}
	}

	threadID := ""
	if opts.ResumeSessionID != "" {
		raw, err := rpc.request(ctx, "thread/resume", map[string]any{
			"threadId": opts.ResumeSessionID,
			"cwd":      opts.WorkDir,
		})
		if err == nil {
			threadID = extractCodexThreadID(raw)
		}
	}
	if threadID == "" {
		raw, err := rpc.request(ctx, "thread/start", map[string]any{"cwd": opts.WorkDir})
		if err != nil {
			return Result{IsError: true, ErrorMessage: "codex thread/start 失败: " + err.Error()}
		}
		threadID = extractCodexThreadID(raw)
		if threadID == "" {
			return Result{IsError: true, ErrorMessage: "codex thread/start 未返回 thread ID"}
		}
	}

	if _, err := rpc.request(ctx, "turn/start", map[string]any{
		"threadId": threadID,
		"input":    []map[string]any{{"type": "text", "text": prompt}},
	}); err != nil {
		return Result{IsError: true, ErrorMessage: "codex turn/start 失败: " + err.Error(), CLISessionID: threadID}
	}

	select {
	case outcome := <-rpc.turnDone:
		result := Result{
			Text:         rpc.finalText(),
			CLISessionID: threadID,
			Usage:        rpc.usage(),
		}
		if outcome.aborted || outcome.errorMessage != "" {
			result.IsError = true
			result.ErrorMessage = outcome.errorMessage
			if result.ErrorMessage == "" {
				result.ErrorMessage = "codex turn 被中止"
			}
		}
		return result
	case <-ctx.Done():
		return Result{IsError: true, ErrorMessage: "codex 执行被取消或超时", CLISessionID: threadID}
	}
}

// codexTurnOutcome 是一轮 turn 的终局。
type codexTurnOutcome struct {
	aborted      bool
	errorMessage string
}

type codexRPCResult struct {
	result json.RawMessage
	err    error
}

// codexRPC 是 app-server 的精简 JSON-RPC 客户端：请求应答配对、
// 通知归一、服务端审批请求自动放行。
type codexRPC struct {
	stdin    interface{ Write([]byte) (int, error) }
	runCtx   context.Context
	messages chan Message
	turnDone chan codexTurnOutcome

	mu         sync.Mutex
	nextID     int64
	pending    map[int64]chan codexRPCResult
	texts      []string
	tokenUsage *TokenUsage
	doneSent   bool
}

// request 发送请求并等待应答。
func (c *codexRPC) request(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	c.nextID++
	id := c.nextID
	waiter := make(chan codexRPCResult, 1)
	c.pending[id] = waiter
	c.mu.Unlock()

	frame, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	if _, err := c.stdin.Write(append(frame, '\n')); err != nil {
		return nil, fmt.Errorf("write %s: %w", method, err)
	}
	select {
	case outcome := <-waiter:
		return outcome.result, outcome.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *codexRPC) respond(id int64, result any) {
	frame, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	if err != nil {
		return
	}
	_, _ = c.stdin.Write(append(frame, '\n'))
}

// readLoop 消费 app-server stdout：应答配对、服务端请求放行、通知归一。
func (c *codexRPC) readLoop(stdout interface{ Read([]byte) (int, error) }) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1<<20), 10<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var frame struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Result json.RawMessage `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			continue
		}
		switch {
		case frame.ID != nil && frame.Method == "": // 应答
			c.mu.Lock()
			waiter := c.pending[*frame.ID]
			delete(c.pending, *frame.ID)
			c.mu.Unlock()
			if waiter != nil {
				outcome := codexRPCResult{result: frame.Result}
				if frame.Error != nil {
					outcome.err = fmt.Errorf("%s", frame.Error.Message)
				}
				waiter <- outcome
			}
		case frame.ID != nil: // 服务端请求：审批一律放行（设备是租户自有信任域）
			c.handleServerRequest(*frame.ID, frame.Method, frame.Params)
		default: // 通知
			c.handleNotification(frame.Method, frame.Params)
		}
	}
	// stdout 关闭（进程退出/被杀）：终局兜底，避免 driveTurn 永久等待。
	c.finishTurn(codexTurnOutcome{aborted: true, errorMessage: "codex app-server 输出流关闭"})
}

func (c *codexRPC) handleServerRequest(id int64, method string, params json.RawMessage) {
	switch method {
	case "item/commandExecution/requestApproval", "execCommandApproval",
		"item/fileChange/requestApproval", "applyPatchApproval":
		c.respond(id, map[string]any{"decision": "accept"})
	case "item/permissions/requestApproval":
		var request struct {
			Permissions map[string]any `json:"permissions"`
		}
		_ = json.Unmarshal(params, &request)
		c.respond(id, map[string]any{"permissions": request.Permissions, "scope": "turn"})
	default:
		frame, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": id,
			"error": map[string]any{"code": -32601, "message": "unsupported request: " + method},
		})
		_, _ = c.stdin.Write(append(frame, '\n'))
	}
}

func (c *codexRPC) handleNotification(method string, rawParams json.RawMessage) {
	var params map[string]any
	_ = json.Unmarshal(rawParams, &params)
	switch method {
	case "turn/completed":
		turn, _ := params["turn"].(map[string]any)
		status, _ := turn["status"].(string)
		if usage, ok := turn["usage"].(map[string]any); ok {
			c.recordUsage(usage)
		}
		outcome := codexTurnOutcome{}
		switch status {
		case "cancelled", "canceled", "aborted", "interrupted":
			outcome.aborted = true
		case "failed":
			outcome.errorMessage = extractCodexErrorMessage(turn)
		}
		c.finishTurn(outcome)
	case "error":
		willRetry, _ := params["willRetry"].(bool)
		message := extractCodexErrorMessage(params)
		if message != "" && !willRetry {
			c.finishTurn(codexTurnOutcome{errorMessage: message})
		}
	case "item/started", "item/completed":
		c.handleItem(method, params)
	}
}

func (c *codexRPC) handleItem(method string, params map[string]any) {
	item, _ := params["item"].(map[string]any)
	if item == nil {
		return
	}
	itemType, _ := item["type"].(string)
	itemID, _ := item["id"].(string)
	switch {
	case method == "item/started" && itemType == "commandExecution":
		command, _ := item["command"].(string)
		input, _ := json.Marshal(map[string]string{"command": command})
		c.emit(Message{Type: MessageToolUse, Tool: "exec_command", CallID: itemID, Input: input})
	case method == "item/completed" && itemType == "commandExecution":
		output, _ := item["aggregatedOutput"].(string)
		c.emit(Message{Type: MessageToolResult, Tool: "exec_command", CallID: itemID, Output: output})
	case method == "item/started" && itemType == "fileChange":
		c.emit(Message{Type: MessageToolUse, Tool: "patch_apply", CallID: itemID})
	case method == "item/completed" && itemType == "fileChange":
		c.emit(Message{Type: MessageToolResult, Tool: "patch_apply", CallID: itemID})
	case method == "item/completed" && itemType == "agentMessage":
		text, _ := item["text"].(string)
		if text != "" {
			c.mu.Lock()
			c.texts = append(c.texts, text)
			c.mu.Unlock()
			c.emit(Message{Type: MessageText, Text: text})
		}
	case method == "item/completed" && itemType == "reasoning":
		text, _ := item["text"].(string)
		if text != "" {
			c.emit(Message{Type: MessageThinking, Text: text})
		}
	}
}

func (c *codexRPC) emit(message Message) {
	select {
	case c.messages <- message:
	case <-c.runCtx.Done():
	}
}

func (c *codexRPC) finishTurn(outcome codexTurnOutcome) {
	c.mu.Lock()
	alreadySent := c.doneSent
	c.doneSent = true
	c.mu.Unlock()
	if alreadySent {
		return
	}
	c.turnDone <- outcome
}

func (c *codexRPC) finalText() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.TrimSpace(strings.Join(c.texts, "\n\n"))
}

func (c *codexRPC) usage() *TokenUsage {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tokenUsage
}

func (c *codexRPC) recordUsage(usage map[string]any) {
	asInt := func(key string) int64 {
		if value, ok := usage[key].(float64); ok {
			return int64(value)
		}
		return 0
	}
	c.mu.Lock()
	c.tokenUsage = &TokenUsage{
		InputTokens:     asInt("input_tokens"),
		OutputTokens:    asInt("output_tokens"),
		CacheReadTokens: asInt("cached_input_tokens"),
	}
	c.mu.Unlock()
}

func extractCodexThreadID(raw json.RawMessage) string {
	var payload struct {
		ThreadID string `json:"threadId"`
		Thread   struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	if payload.ThreadID != "" {
		return payload.ThreadID
	}
	return payload.Thread.ID
}

func extractCodexErrorMessage(container map[string]any) string {
	if errorMap, ok := container["error"].(map[string]any); ok {
		if message, ok := errorMap["message"].(string); ok {
			return message
		}
	}
	if message, ok := container["message"].(string); ok {
		return message
	}
	return ""
}
