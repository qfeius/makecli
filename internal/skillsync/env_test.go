/**
 * [INPUT]: 依赖 errors、strings、testing
 * [OUTPUT]: 覆盖 EnsureNpx 的存在/缺失路径；提供 stubNpxPresent / stubNpxMissing 供 sync/remove/install 测试复用
 * [POS]: internal/skillsync 环境门禁层测试，lookPathFunc 打桩隔离 PATH
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package skillsync

import (
	"errors"
	"strings"
	"testing"
)

// stubNpxPresent 打桩 lookPathFunc 模拟 npx 存在，被 sync/remove/install 测试复用保持隔离。
func stubNpxPresent(t *testing.T) {
	t.Helper()
	orig := lookPathFunc
	lookPathFunc = func(file string) (string, error) { return "/usr/local/bin/" + file, nil }
	t.Cleanup(func() { lookPathFunc = orig })
}

// stubNpxMissing 打桩 lookPathFunc 模拟 npx 缺失。
func stubNpxMissing(t *testing.T) {
	t.Helper()
	orig := lookPathFunc
	lookPathFunc = func(string) (string, error) { return "", errors.New("executable file not found in $PATH") }
	t.Cleanup(func() { lookPathFunc = orig })
}

func TestEnsureNpxPresent(t *testing.T) {
	stubNpxPresent(t)
	if err := EnsureNpx(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEnsureNpxMissingGivesGuidance(t *testing.T) {
	stubNpxMissing(t)
	err := EnsureNpx()
	if err == nil {
		t.Fatal("expected error when npx missing")
	}
	for _, want := range []string{"npx not found", "How to fix", "brew install node", "https://nodejs.org"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error missing %q:\n%s", want, err)
		}
	}
}
