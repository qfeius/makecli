/**
 * [INPUT]: 依赖 repl.go 与 client.go；httptest 起假 gateway
 * [OUTPUT]: 对外提供会话编排回归——RunOnce 流式输出、REPL 多轮历史携带与 /exit /clear
 * [POS]: internal/agent 的测试面（编排层）
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package agent

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newEchoGateway 假 gateway：每轮把收到的 messages 数量与最后一条内容回显为增量。
func newEchoGateway(t *testing.T) (*httptest.Server, *[][]Message) {
	t.Helper()
	var rounds [][]Message
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, _ := io.ReadAll(r.Body)
		var request struct {
			Messages []Message `json:"messages"`
		}
		_ = json.Unmarshal(payload, &request)
		rounds = append(rounds, request.Messages)
		w.Header().Set("Content-Type", "text/event-stream")
		reply, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{"delta": map[string]string{"content": "回复" + request.Messages[len(request.Messages)-1].Content}}},
		})
		_, _ = w.Write([]byte("data: " + string(reply) + "\n\ndata: [DONE]\n\n"))
	}))
	t.Cleanup(server.Close)
	return server, &rounds
}

func TestRunOnceStreamsReply(t *testing.T) {
	server, rounds := newEchoGateway(t)
	var output strings.Builder
	err := RunOnce(context.Background(), NewClient(server.URL, "tok", "s"), "default", "你是助手", "你好", &output)
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if !strings.Contains(output.String(), "回复你好") {
		t.Errorf("output = %q", output.String())
	}
	if len(*rounds) != 1 || (*rounds)[0][0].Role != "system" {
		t.Errorf("system prompt 未携带: %+v", *rounds)
	}
}

func TestREPLCarriesHistoryAndExits(t *testing.T) {
	server, rounds := newEchoGateway(t)
	input := strings.NewReader("第一轮\n第二轮\n/exit\n")
	var output strings.Builder
	err := RunREPL(context.Background(), NewClient(server.URL, "tok", "s"), "default", "", input, &output)
	if err != nil {
		t.Fatalf("RunREPL: %v", err)
	}
	if len(*rounds) != 2 {
		t.Fatalf("rounds = %d, want 2", len(*rounds))
	}
	// 第二轮请求应携带第一轮的 user + assistant 历史。
	second := (*rounds)[1]
	if len(second) != 3 || second[0].Content != "第一轮" || second[1].Content != "回复第一轮" || second[2].Content != "第二轮" {
		t.Errorf("历史未携带: %+v", second)
	}
}

func TestREPLClearResetsHistory(t *testing.T) {
	server, rounds := newEchoGateway(t)
	input := strings.NewReader("第一轮\n/clear\n第二轮\n/quit\n")
	var output strings.Builder
	if err := RunREPL(context.Background(), NewClient(server.URL, "tok", "s"), "default", "", input, &output); err != nil {
		t.Fatalf("RunREPL: %v", err)
	}
	second := (*rounds)[1]
	if len(second) != 1 || second[0].Content != "第二轮" {
		t.Errorf("/clear 未清空历史: %+v", second)
	}
	if !strings.Contains(output.String(), "历史已清空") {
		t.Errorf("output = %q", output.String())
	}
}
