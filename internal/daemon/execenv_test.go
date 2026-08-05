/**
 * [INPUT]: 依赖 execenv.go 的 PrepareWorkDir/BuildPrompt
 * [OUTPUT]: 对外提供执行环境回归——工作目录连续性优先、description 身份职责与 instructions 执行要求双文件渲染、触发区间 prompt 合并
 * [POS]: internal/daemon 的 execenv 测试面
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPrepareWorkDirRendersInstructions(t *testing.T) {
	base := t.TempDir()
	claim := RunClaim{
		SessionID: "session_1",
		Agent: AgentBundle{
			Name: "助手", Description: "SRE 专家，精通 Kubernetes 与云原生。",
			Instructions: "永远说中文",
		},
	}
	workDir, resumable, err := PrepareWorkDir(base, claim)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if workDir != filepath.Join(base, "session_1") || !resumable {
		t.Fatalf("workDir = %q resumable = %v", workDir, resumable)
	}
	for _, name := range []string{"CLAUDE.md", "AGENTS.md"} {
		content, err := os.ReadFile(filepath.Join(workDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		want := "# 助手\n\n## 身份与职责\n\nSRE 专家，精通 Kubernetes 与云原生。\n\n## 执行要求\n\n永远说中文\n"
		if string(content) != want {
			t.Fatalf("%s = %q", name, content)
		}
	}
}

func TestPrepareWorkDirRendersDescriptionWithoutInstructions(t *testing.T) {
	base := t.TempDir()
	claim := RunClaim{
		SessionID: "session_description",
		Agent:     AgentBundle{Name: "SRE", Description: "负责 k8s 运维。"},
	}
	workDir, _, err := PrepareWorkDir(base, claim)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	content, err := os.ReadFile(filepath.Join(workDir, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if want := "# SRE\n\n## 身份与职责\n\n负责 k8s 运维。\n"; string(content) != want {
		t.Fatalf("AGENTS.md = %q, want %q", content, want)
	}
}

func TestPrepareWorkDirPrefersResumeDir(t *testing.T) {
	resumeDir := filepath.Join(t.TempDir(), "existing")
	claim := RunClaim{SessionID: "session_1", Resume: ResumeState{WorkDir: resumeDir}}
	workDir, resumable, err := PrepareWorkDir(t.TempDir(), claim)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if workDir != resumeDir || !resumable {
		t.Fatalf("连续性 workDir 应沿用: %q resumable=%v", workDir, resumable)
	}
}

func TestPrepareWorkDirFallsBackOnUnusableResumeDir(t *testing.T) {
	// 跨设备遗留路径（如另一台机器的 /Users/...）不可创建时回退新目录并放弃连续性。
	base := t.TempDir()
	claim := RunClaim{SessionID: "session_1", Resume: ResumeState{WorkDir: "/nonexistent-root/child", CLISessionID: "cli_stale"}}
	workDir, resumable, err := PrepareWorkDir(base, claim)
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	if workDir != filepath.Join(base, "session_1") || resumable {
		t.Fatalf("应回退新目录且 resumable=false: %q %v", workDir, resumable)
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

type fakeBlobFetcher struct {
	contents map[string][]byte
	failRefs map[string]bool
	calls    int
}

func (f *fakeBlobFetcher) FetchBlob(_ context.Context, ref string, _ int64) ([]byte, error) {
	f.calls++
	if f.failRefs[ref] {
		return nil, fmt.Errorf("回源失败")
	}
	content, found := f.contents[ref]
	if !found {
		return nil, fmt.Errorf("不存在")
	}
	return content, nil
}

func skillBundles() []SkillBundle {
	return []SkillBundle{{
		Name: "pdf-report", Description: "生成 PDF 报表时使用", Body: "# 步骤\n1. 跑脚本",
		Files: []SkillFileBundle{{Path: "scripts/render.py", BlobRef: "blob_a"}},
	}}
}

func TestRenderSkillsWritesNativeDiscoveryPaths(t *testing.T) {
	workDir := t.TempDir()
	fetcher := &fakeBlobFetcher{contents: map[string][]byte{"blob_a": []byte("print('hi')")}}
	warnings, err := RenderSkills(context.Background(), workDir, skillBundles(), fetcher)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("全部就位时不该有告警: %v", warnings)
	}
	for _, root := range []string{".claude/skills", ".agents/skills"} {
		markdown, err := os.ReadFile(filepath.Join(workDir, root, "pdf-report", "SKILL.md"))
		if err != nil {
			t.Fatalf("读 %s 的 SKILL.md: %v", root, err)
		}
		// frontmatter 是各 CLI 渐进披露的索引层，必须在。
		for _, want := range []string{"---", "name: pdf-report", "description: 生成 PDF 报表时使用", "跑脚本"} {
			if !strings.Contains(string(markdown), want) {
				t.Errorf("%s 的 SKILL.md 缺 %q:\n%s", root, want, markdown)
			}
		}
		script, err := os.ReadFile(filepath.Join(workDir, root, "pdf-report", "scripts", "render.py"))
		if err != nil || string(script) != "print('hi')" {
			t.Errorf("%s 的附件未就位: %q %v", root, script, err)
		}
	}
	// 附件只回源一次，两个根目录共用——省掉一半网络往返。
	if fetcher.calls != 1 {
		t.Errorf("附件应只回源一次, got %d", fetcher.calls)
	}
}

// TestRenderSkillsRemovesUnboundSkills 是本函数存在的根本理由：workDir 跨 run
// 复用，增量补写会让上一轮解绑的技能阴魂不散——CLI 仍会发现它、仍会照着它
// 行事，而管理台上它已经不在了。
func TestRenderSkillsRemovesUnboundSkills(t *testing.T) {
	workDir := t.TempDir()
	fetcher := &fakeBlobFetcher{contents: map[string][]byte{"blob_a": []byte("x")}}
	if _, err := RenderSkills(context.Background(), workDir, skillBundles(), fetcher); err != nil {
		t.Fatalf("首轮: %v", err)
	}
	stale := filepath.Join(workDir, ".claude", "skills", "pdf-report", "SKILL.md")
	if _, err := os.Stat(stale); err != nil {
		t.Fatalf("首轮应写入: %v", err)
	}

	// 第二轮：agent 已解绑全部技能。
	if _, err := RenderSkills(context.Background(), workDir, nil, fetcher); err != nil {
		t.Fatalf("次轮: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("解绑后旧技能必须从磁盘消失")
	}

	// 第三轮：换成另一个技能，旧的同样不能残留。
	replacement := []SkillBundle{{Name: "other-skill", Body: "b"}}
	if _, err := RenderSkills(context.Background(), workDir, replacement, fetcher); err != nil {
		t.Fatalf("三轮: %v", err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("被替换的技能不该残留")
	}
	if _, err := os.Stat(filepath.Join(workDir, ".claude", "skills", "other-skill", "SKILL.md")); err != nil {
		t.Errorf("新技能应就位: %v", err)
	}
}

func TestRenderSkillsSurvivesFetchFailure(t *testing.T) {
	workDir := t.TempDir()
	fetcher := &fakeBlobFetcher{failRefs: map[string]bool{"blob_a": true}}
	warnings, err := RenderSkills(context.Background(), workDir, skillBundles(), fetcher)
	if err != nil {
		t.Fatalf("回源失败不该整体报错: %v", err)
	}
	if len(warnings) == 0 || !strings.Contains(warnings[0], "scripts/render.py") {
		t.Errorf("应点名未就位的附件: %v", warnings)
	}
	// 正文仍要写下去——它本身就有用。
	if _, err := os.Stat(filepath.Join(workDir, ".claude", "skills", "pdf-report", "SKILL.md")); err != nil {
		t.Errorf("正文仍应写入: %v", err)
	}
}

func TestRenderSkillsRejectsTraversal(t *testing.T) {
	workDir := t.TempDir()
	fetcher := &fakeBlobFetcher{contents: map[string][]byte{"blob_a": []byte("x")}}
	evil := []SkillBundle{{
		Name: "ok-name", Body: "b",
		Files: []SkillFileBundle{{Path: "../../escape.txt", BlobRef: "blob_a"}},
	}}
	warnings, err := RenderSkills(context.Background(), workDir, evil, fetcher)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if len(warnings) == 0 {
		t.Error("遍历路径应产生告警")
	}
	if fetcher.calls != 0 {
		t.Error("形状非法应在回源之前挡住")
	}
	if _, err := os.Stat(filepath.Join(workDir, "escape.txt")); !os.IsNotExist(err) {
		t.Error("遍历路径不该落地")
	}

	// 技能名本身带分隔符同样要挡住。
	named := []SkillBundle{{Name: "../evil", Body: "b"}}
	warnings, _ = RenderSkills(context.Background(), workDir, named, fetcher)
	if len(warnings) == 0 {
		t.Error("非法技能名应产生告警")
	}
}
