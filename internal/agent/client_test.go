/**
 * [INPUT]: 依赖 client.go；httptest 隔离网络
 * [OUTPUT]: 对外提供传输层回归——SSE 增量解析、鉴权头/会话头、OpenAI 错误体还原
 * [POS]: internal/agent 的测试面（传输层）
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatStreamParsesDeltasAndHeaders(t *testing.T) {
	var gotAuth, gotSession, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotSession = r.Header.Get("X-Session-ID")
		buffer := make([]byte, 4096)
		n, _ := r.Body.Read(buffer)
		gotBody = string(buffer[:n])
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"你\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"好\"}}]}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer server.Close()

	client := NewClient(server.URL, "tok-1", "session_local_ab")
	var deltas []string
	full, err := client.ChatStream(context.Background(), "default",
		[]Message{{Role: "user", Content: "hi"}}, func(d string) { deltas = append(deltas, d) })
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	if full != "你好" || strings.Join(deltas, "") != "你好" {
		t.Errorf("full=%q deltas=%v", full, deltas)
	}
	if gotAuth != "Bearer tok-1" || gotSession != "session_local_ab" {
		t.Errorf("headers auth=%q session=%q", gotAuth, gotSession)
	}
	if !strings.Contains(gotBody, `"model":"default"`) || !strings.Contains(gotBody, `"stream":true`) {
		t.Errorf("body = %s", gotBody)
	}
}

func TestChatStreamDecodesOpenAIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"凭证无效或已吊销","type":"authentication_error"}}`))
	}))
	defer server.Close()

	_, err := NewClient(server.URL, "bad", "s").ChatStream(context.Background(), "default", nil, nil)
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.HTTPStatus != http.StatusUnauthorized || apiErr.Type != "authentication_error" {
		t.Errorf("err = %v", err)
	}
}

func TestNewSessionIDUnique(t *testing.T) {
	first, second := NewSessionID(), NewSessionID()
	if first == second {
		t.Error("session id 应当随机")
	}
	if !strings.HasPrefix(NewSessionID(), "session_local_") {
		t.Error("session id 前缀错误")
	}
}
