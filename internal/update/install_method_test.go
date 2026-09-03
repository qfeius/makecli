/**
 * [INPUT]: 依赖 testing、slices；internal/update 自身
 * [OUTPUT]: 单元测试，无导出
 * [POS]: 覆盖 installMethodFromPath 的路径分类（裸/npm/pnpm 两种布局/Windows 分隔符）、packageManagerCommand 命令拼装、applyViaPackageManager 经 runCommand seam 的委托与错误包装
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package update

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestInstallMethodFromPath(t *testing.T) {
	tests := []struct {
		path string
		want InstallMethod
	}{
		{"/opt/homebrew/Cellar/makecli/0.5.9/bin/makecli", InstallBinary},
		{"/usr/local/bin/makecli", InstallBinary},
		{"/home/u/pnpm-tools/bin/makecli", InstallBinary},
		{"/usr/local/lib/node_modules/@qfeius/makecli-darwin-arm64/bin/makecli", InstallNpm},
		{`C:\Users\u\AppData\Roaming\npm\node_modules\@qfeius\makecli-win32-x64\bin\makecli.exe`, InstallNpm},
		{"/home/u/.local/share/pnpm/global/5/node_modules/.pnpm/@qfeius+makecli-linux-x64@0.5.9/node_modules/@qfeius/makecli-linux-x64/bin/makecli", InstallPnpm},
		{"/Users/u/Library/pnpm/store/v11/links/ab/node_modules/@qfeius/makecli-darwin-arm64/bin/makecli", InstallPnpm},
		{"/srv/pnpm/node_modules/@qfeius/makecli-linux-x64/bin/makecli", InstallNpm},
	}
	for _, tt := range tests {
		if got := installMethodFromPath(tt.path); got != tt.want {
			t.Errorf("installMethodFromPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestPackageManagerCommand(t *testing.T) {
	if got := packageManagerCommand(InstallNpm, "0.5.9"); !slices.Equal(got, []string{"npm", "install", "-g", "@qfeius/makecli@0.5.9"}) {
		t.Errorf("npm command = %v", got)
	}
	if got := packageManagerCommand(InstallPnpm, "0.6.0-beta.1"); !slices.Equal(got, []string{"pnpm", "add", "-g", "@qfeius/makecli@0.6.0-beta.1"}) {
		t.Errorf("pnpm command = %v", got)
	}
}

func TestApplyViaPackageManager(t *testing.T) {
	old := runCommand
	t.Cleanup(func() { runCommand = old })

	var gotName string
	var gotArgs []string
	runCommand = func(name string, args ...string) error {
		gotName, gotArgs = name, args
		return nil
	}
	// npm 在开发机上必然存在；若缺失则跳过而非误报
	if err := applyViaPackageManager(InstallNpm, "0.5.9"); err != nil {
		if strings.Contains(err.Error(), "not on PATH") {
			t.Skip("npm not on PATH")
		}
		t.Fatalf("applyViaPackageManager: %v", err)
	}
	if gotName != "npm" || !slices.Equal(gotArgs, []string{"install", "-g", "@qfeius/makecli@0.5.9"}) {
		t.Errorf("delegated to %s %v", gotName, gotArgs)
	}

	runCommand = func(string, ...string) error { return errors.New("exit status 1") }
	err := applyViaPackageManager(InstallNpm, "0.5.9")
	if err == nil || !strings.Contains(err.Error(), "npm install -g @qfeius/makecli@0.5.9 failed") {
		t.Errorf("error should name the failed command, got %v", err)
	}
}
