/**
 * [INPUT]: 依赖 io、os、path/filepath、strings、testing；包内 atomicWrite（白盒）
 * [OUTPUT]: 单元测试，无导出
 * [POS]: internal/config 原子写 helper 的测试，覆盖落盘内容、权限、无临时文件残留、覆盖既有文件
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package config

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.ini")

	err := atomicWrite(path, 0600, func(w io.Writer) error {
		_, err := io.WriteString(w, "hello atomic\n")
		return err
	})
	if err != nil {
		t.Fatalf("atomicWrite: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "hello atomic\n" {
		t.Errorf("content = %q, want %q", got, "hello atomic\n")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("perm = %v, want 0600", info.Mode().Perm())
	}

	// 不得残留临时文件
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.Name() != "out.ini" {
			t.Errorf("leaked temp file: %s", e.Name())
		}
	}
}

func TestAtomicWriteOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.ini")
	if err := os.WriteFile(path, []byte("OLD CONTENT that is long\n"), 0600); err != nil {
		t.Fatal(err)
	}

	err := atomicWrite(path, 0600, func(w io.Writer) error {
		_, err := io.WriteString(w, "NEW\n")
		return err
	})
	if err != nil {
		t.Fatalf("atomicWrite: %v", err)
	}

	got, _ := os.ReadFile(path)
	if string(got) != "NEW\n" {
		t.Errorf("content = %q, want fully replaced %q", got, "NEW\n")
	}
}

// TestAtomicWritePropagatesRenderError 验证 render 出错时不落盘、不残留临时文件
func TestAtomicWritePropagatesRenderError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.ini")

	wantErr := io.ErrClosedPipe
	err := atomicWrite(path, 0600, func(w io.Writer) error {
		_, _ = io.WriteString(w, "partial")
		return wantErr
	})
	if err == nil || !strings.Contains(err.Error(), "closed pipe") {
		t.Fatalf("err = %v, want render error propagated", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Error("target must not exist when render fails")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("no temp file should remain on render error, got %v", entries)
	}
}
