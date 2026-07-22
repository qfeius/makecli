/**
 * [INPUT]: 依赖 execenv.go 的 PrepareWorkDir/BuildPrompt
 * [OUTPUT]: 对外提供执行环境回归——工作目录连续性优先、instructions 双文件渲染、触发区间 prompt 合并
 * [POS]: internal/daemon 的 execenv 测试面
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestPrepareWorkDirRendersInstructions(t *testing.T) {
	base := t.TempDir()
	claim := RunClaim{
		SessionID: "session_1",
		Agent:     AgentBundle{Name: "助手", Instructions: "永远说中文"},
	}
	workDir, err := PrepareWorkDir(base, claim)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if workDir != filepath.Join(base, "session_1") {
		t.Fatalf("workDir = %q", workDir)
	}
	for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
		content, err := os.ReadFile(filepath.Join(workDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(content) != "# 助手\n\n永远说中文\n" {
			t.Fatalf("%s = %q", name, content)
		}
	}
}

func TestPrepareWorkDirPrefersResumeDir(t *testing.T) {
	resumeDir := filepath.Join(t.TempDir(), "existing")
	claim := RunClaim{SessionID: "session_1", Resume: ResumeState{WorkDir: resumeDir}}
	workDir, err := PrepareWorkDir(t.TempDir(), claim)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if workDir != resumeDir {
		t.Fatalf("连续性 work_dir 应沿用: %q", workDir)
	}
}

func TestBuildPromptMergesTriggerRange(t *testing.T) {
	payload := func(text string) []byte {
		raw, _ := json.Marshal(UserMessagePayload{Blocks: []Block{
			{Kind: "mention", Text: "助手"},
			{Kind: "text", Text: text},
		}})
		return raw
	}
	events := []Event{
		{Seq: 0, Type: "user_message", Payload: payload("先看这个")},
		{Seq: 1, Type: "run_started"}, // 非 user_message 跳过
		{Seq: 2, Type: "user_message", Payload: payload("再看那个")},
	}
	prompt := BuildPrompt(events)
	if prompt != "@助手 先看这个\n\n@助手 再看那个" {
		t.Fatalf("prompt = %q", prompt)
	}
}
