/**
 * [INPUT]: 依赖 codex.go 的 codexRPC 与 driveTurn 所用协议原语；io.Pipe 模拟 app-server stdio
 * [OUTPUT]: 对外提供 JSON-RPC 客户端回归——应答配对、通知归一、审批自动放行、turn 终局与文本聚合
 * [POS]: internal/daemon/adapter 的 codex 测试面——协议语义锁定，不依赖真实 codex 二进制
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package adapter

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"
)

// fakeAppServer 用管道模拟 app-server：读请求、按脚本回帧。
type fakeAppServer struct {
	rpc      *codexRPC
	toRPC    *io.PipeWriter // 服务端 → 客户端
	fromRPC  *bufio.Scanner // 客户端 → 服务端
	messages chan Message
}

func newFakeAppServer(t *testing.T) *fakeAppServer {
	t.Helper()
	serverReader, clientWriter := io.Pipe() // rpc.stdin 写入端
	clientReader, serverWriter := io.Pipe() // rpc.readLoop 读取端
	messages := make(chan Message, 64)
	rpc := &codexRPC{
		stdin:    clientWriter,
		pending:  map[int64]chan codexRPCResult{},
		turnDone: make(chan codexTurnOutcome, 1),
		messages: messages,
		runCtx:   context.Background(),
	}
	go rpc.readLoop(clientReader)
	return &fakeAppServer{
		rpc:      rpc,
		toRPC:    serverWriter,
		fromRPC:  bufio.NewScanner(serverReader),
		messages: messages,
	}
}

func (f *fakeAppServer) send(t *testing.T, frame string) {
	t.Helper()
	if _, err := f.toRPC.Write([]byte(frame + "\n")); err != nil {
		t.Fatalf("send frame: %v", err)
	}
}

func (f *fakeAppServer) readFrame(t *testing.T) map[string]any {
	t.Helper()
	if !f.fromRPC.Scan() {
		t.Fatal("客户端写入流已关闭")
	}
	var frame map[string]any
	if err := json.Unmarshal(f.fromRPC.Bytes(), &frame); err != nil {
		t.Fatalf("客户端帧非 JSON: %v", err)
	}
	return frame
}

func TestCodexRequestResponsePairing(t *testing.T) {
	server := newFakeAppServer(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		frame := server.readFrame(t)
		if frame["method"] != "initialize" {
			t.Errorf("method = %v", frame["method"])
		}
		id := int64(frame["id"].(float64))
		response, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"ok": true}})
		server.send(t, string(response))
	}()
	result, err := server.rpc.request(context.Background(), "initialize", map[string]any{})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if string(result) == "" {
		t.Fatal("应答 result 为空")
	}
	<-done
}

func TestCodexApprovalAutoAccept(t *testing.T) {
	server := newFakeAppServer(t)
	server.send(t, `{"jsonrpc":"2.0","id":9,"method":"item/commandExecution/requestApproval","params":{}}`)
	frame := server.readFrame(t)
	if frame["id"].(float64) != 9 {
		t.Fatalf("应答 id = %v", frame["id"])
	}
	result := frame["result"].(map[string]any)
	if result["decision"] != "accept" {
		t.Fatalf("审批应自动放行: %v", result)
	}
}

func TestCodexNotificationsToMessagesAndTurnDone(t *testing.T) {
	server := newFakeAppServer(t)
	server.send(t, `{"jsonrpc":"2.0","method":"item/started","params":{"item":{"id":"i1","type":"commandExecution","command":"ls"}}}`)
	server.send(t, `{"jsonrpc":"2.0","method":"item/completed","params":{"item":{"id":"i1","type":"commandExecution","aggregatedOutput":"file.txt"}}}`)
	server.send(t, `{"jsonrpc":"2.0","method":"item/completed","params":{"item":{"id":"i2","type":"agentMessage","text":"结论在此"}}}`)
	server.send(t, `{"jsonrpc":"2.0","method":"turn/completed","params":{"turn":{"status":"completed","usage":{"input_tokens":50,"output_tokens":7}}}}`)

	var received []Message
	timeout := time.After(3 * time.Second)
	for len(received) < 3 {
		select {
		case message := <-server.messages:
			received = append(received, message)
		case <-timeout:
			t.Fatalf("只收到 %d 条消息", len(received))
		}
	}
	if received[0].Type != MessageToolUse || received[0].Tool != "exec_command" {
		t.Fatalf("tool_use = %+v", received[0])
	}
	if received[1].Type != MessageToolResult || received[1].Output != "file.txt" {
		t.Fatalf("tool_result = %+v", received[1])
	}
	if received[2].Type != MessageText || received[2].Text != "结论在此" {
		t.Fatalf("text = %+v", received[2])
	}

	select {
	case outcome := <-server.rpc.turnDone:
		if outcome.aborted || outcome.errorMessage != "" {
			t.Fatalf("outcome = %+v", outcome)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("turn/completed 应触发终局")
	}
	if text := server.rpc.finalText(); text != "结论在此" {
		t.Fatalf("finalText = %q", text)
	}
	usage := server.rpc.usage()
	if usage == nil || usage.InputTokens != 50 || usage.OutputTokens != 7 {
		t.Fatalf("usage = %+v", usage)
	}
}

func TestCodexFailedTurnCarriesError(t *testing.T) {
	server := newFakeAppServer(t)
	server.send(t, `{"jsonrpc":"2.0","method":"turn/completed","params":{"turn":{"status":"failed","error":{"message":"配额耗尽"}}}}`)
	select {
	case outcome := <-server.rpc.turnDone:
		if outcome.errorMessage != "配额耗尽" {
			t.Fatalf("outcome = %+v", outcome)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("失败 turn 应触发终局")
	}
}

func TestCodexStreamCloseFinishesTurn(t *testing.T) {
	// 进程意外退出（stdout 关闭）必须触发终局兜底，driveTurn 不悬挂。
	server := newFakeAppServer(t)
	_ = server.toRPC.Close()
	select {
	case outcome := <-server.rpc.turnDone:
		if !outcome.aborted {
			t.Fatalf("outcome = %+v", outcome)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("输出流关闭应触发终局")
	}
}

func TestExtractCodexThreadID(t *testing.T) {
	if id := extractCodexThreadID([]byte(`{"threadId":"th_1"}`)); id != "th_1" {
		t.Fatalf("id = %q", id)
	}
	if id := extractCodexThreadID([]byte(`{"thread":{"id":"th_2"}}`)); id != "th_2" {
		t.Fatalf("id = %q", id)
	}
}
