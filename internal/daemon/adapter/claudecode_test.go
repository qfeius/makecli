/**
 * [INPUT]: 依赖 claudecode.go 的 parseClaudeLine/flattenToolResultContent
 * [OUTPUT]: 对外提供 stream-json 解析的 golden 回归——assistant 块归一、tool_result 配对、result 收口、未知行跳过
 * [POS]: internal/daemon/adapter 的 claude-code 测试面——锁定解析语义不被 CLI 版本演进或重构漂移
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package adapter

import "testing"

func TestParseClaudeAssistantBlocks(t *testing.T) {
	line := `{"type":"assistant","message":{"content":[` +
		`{"type":"thinking","thinking":"想一想"},` +
		`{"type":"text","text":"我来看看"},` +
		`{"type":"tool_use","id":"call_1","name":"Bash","input":{"command":"ls"}}]}}`
	messages, result, ok := parseClaudeLine(line)
	if !ok || result != nil {
		t.Fatalf("ok=%v result=%v", ok, result)
	}
	if len(messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(messages))
	}
	if messages[0].Type != MessageThinking || messages[0].Text != "想一想" {
		t.Fatalf("thinking = %+v", messages[0])
	}
	if messages[1].Type != MessageText || messages[1].Text != "我来看看" {
		t.Fatalf("text = %+v", messages[1])
	}
	if messages[2].Type != MessageToolUse || messages[2].Tool != "Bash" || messages[2].CallID != "call_1" {
		t.Fatalf("tool_use = %+v", messages[2])
	}
}

func TestParseClaudeToolResult(t *testing.T) {
	line := `{"type":"user","message":{"content":[` +
		`{"type":"tool_result","tool_use_id":"call_1","content":[{"type":"text","text":"file.txt"}],"is_error":false}]}}`
	messages, _, ok := parseClaudeLine(line)
	if !ok || len(messages) != 1 {
		t.Fatalf("messages = %+v", messages)
	}
	if messages[0].Type != MessageToolResult || messages[0].CallID != "call_1" || messages[0].Output != "file.txt" {
		t.Fatalf("tool_result = %+v", messages[0])
	}
}

func TestParseClaudeResult(t *testing.T) {
	line := `{"type":"result","subtype":"success","session_id":"cli_abc","result":"搞定了",` +
		`"is_error":false,"usage":{"input_tokens":100,"output_tokens":20,"cache_read_input_tokens":5}}`
	_, result, ok := parseClaudeLine(line)
	if !ok || result == nil {
		t.Fatal("result 事件应收口")
	}
	if result.Text != "搞定了" || result.CLISessionID != "cli_abc" || result.IsError {
		t.Fatalf("result = %+v", result)
	}
	if result.Usage == nil || result.Usage.InputTokens != 100 || result.Usage.CacheReadTokens != 5 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

func TestParseClaudeErrorResult(t *testing.T) {
	line := `{"type":"result","subtype":"error_max_turns","session_id":"cli_x","is_error":true}`
	_, result, ok := parseClaudeLine(line)
	if !ok || result == nil || !result.IsError || result.ErrorMessage == "" {
		t.Fatalf("失败 result 应带原因: %+v", result)
	}
}

func TestParseClaudeUnknownLineSkipped(t *testing.T) {
	// CLI 版本演进出现的新事件类型静默跳过，不打断执行。
	for _, line := range []string{`not-json`, `{"type":"future_event"}`} {
		messages, result, ok := parseClaudeLine(line)
		if ok || messages != nil || result != nil {
			t.Fatalf("未知行应跳过: %q", line)
		}
	}
}

func TestFlattenToolResultString(t *testing.T) {
	if got := flattenToolResultContent([]byte(`"plain"`)); got != "plain" {
		t.Fatalf("got %q", got)
	}
}
