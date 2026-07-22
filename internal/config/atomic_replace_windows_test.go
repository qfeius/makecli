//go:build windows

/**
 * [INPUT]: 依赖包内 ReplaceFile/renameFile（白盒打桩）、errors、os、path/filepath、testing
 * [OUTPUT]: 覆盖 Windows 替换的失败合同——重复失败后 dst 原样保有旧内容、路径全程存在、重试计数正确
 * [POS]: internal/config 的 Windows 平台分支测试，用 renameFile 注入失败模拟目标被独占，不依赖真实文件锁
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestReplaceFileFailureKeepsDestination 锁定失败合同：替换失败的每一步 dst 都原样
// 保有旧内容——绝无「旧文件已失、新文件未至」的中间态（曾用删除后重试实现丢过数据）。
func TestReplaceFileFailureKeepsDestination(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.tmp")
	dst := filepath.Join(dir, "dst.conf")
	if err := os.WriteFile(src, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}

	injected := errors.New("sharing violation (injected)")
	calls := 0
	orig := renameFile
	renameFile = func(from, to string) error {
		calls++
		// 每次尝试时 dst 都必须原样在位——失败注入点即合同检查点
		if b, err := os.ReadFile(dst); err != nil || string(b) != "old" {
			t.Errorf("attempt %d: dst content = %q err=%v, want old content intact", calls, b, err)
		}
		return injected
	}
	t.Cleanup(func() { renameFile = orig })

	if err := ReplaceFile(src, dst); !errors.Is(err, injected) {
		t.Fatalf("ReplaceFile = %v, want injected error", err)
	}
	if calls != replaceRetryCount {
		t.Errorf("rename attempts = %d, want %d", calls, replaceRetryCount)
	}
	if b, _ := os.ReadFile(dst); string(b) != "old" {
		t.Errorf("dst after failure = %q, want old content intact", b)
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("src should remain for caller cleanup, stat err: %v", err)
	}
}

// TestReplaceFileRetrySucceeds 前两次失败、第三次成功——重试路径的成功出口。
func TestReplaceFileRetrySucceeds(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.tmp")
	dst := filepath.Join(dir, "dst.conf")
	if err := os.WriteFile(src, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}

	calls := 0
	orig := renameFile
	renameFile = func(from, to string) error {
		calls++
		if calls < 3 {
			return errors.New("busy (injected)")
		}
		return os.Rename(from, to)
	}
	t.Cleanup(func() { renameFile = orig })

	if err := ReplaceFile(src, dst); err != nil {
		t.Fatalf("ReplaceFile: %v", err)
	}
	if b, _ := os.ReadFile(dst); string(b) != "new" {
		t.Errorf("dst after success = %q, want new content", b)
	}
}
