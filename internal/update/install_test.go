/**
 * [INPUT]: 依赖 fmt、os、path/filepath、strings、testing；注入包级 renameFile 钩子
 * [OUTPUT]: 验证 installBinary 原子安装、stage 清理、回滚成功、回滚失败恢复提示
 * [POS]: internal/update 的安装逻辑测试，替代不可行的 replaceBinary 真实二进制替换测试
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package update

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeExec 写一个可执行文件
func writeExec(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// stubRename 安装一个 renameFile 桩：failCalls 中列出的调用序号返回错误，其余委托
// 真实 os.Rename（保证文件系统状态一致）。返回还原函数。
// 调用序：1=备份(exe→.old) 2=安装(stage→exe) 3=回滚(.old→exe)。
func stubRename(failCalls ...int) func() {
	orig := renameFile
	shouldFail := map[int]bool{}
	for _, c := range failCalls {
		shouldFail[c] = true
	}
	calls := 0
	renameFile = func(oldPath, newPath string) error {
		calls++
		if shouldFail[calls] {
			return fmt.Errorf("simulated rename failure on call %d", calls)
		}
		return os.Rename(oldPath, newPath)
	}
	return func() { renameFile = orig }
}

// TestInstallBinaryReplaces 验证 happy path：内容被替换，无 stage / backup 残留
func TestInstallBinaryReplaces(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "makecli")
	writeExec(t, exe, "OLD")
	newBin := filepath.Join(t.TempDir(), "new")
	writeExec(t, newBin, "NEW")

	if err := installBinary(newBin, exe); err != nil {
		t.Fatalf("installBinary: %v", err)
	}

	got, _ := os.ReadFile(exe)
	if string(got) != "NEW" {
		t.Errorf("exe content = %q, want NEW", got)
	}
	if _, err := os.Stat(filepath.Join(dir, ".makecli.tmp")); !os.IsNotExist(err) {
		t.Error("stage file .makecli.tmp leaked after success")
	}
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Error("backup file .old leaked after success")
	}
}

// TestInstallBinaryCleansStageOnBackupFailure 验证备份(步骤2)失败时 stage 被 defer 清理
func TestInstallBinaryCleansStageOnBackupFailure(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "makecli") // 故意不创建：备份 rename 因源缺失而失败
	newBin := filepath.Join(t.TempDir(), "new")
	writeExec(t, newBin, "NEW")

	err := installBinary(newBin, exe)
	if err == nil {
		t.Fatal("expected backup failure, got nil")
	}
	if !strings.Contains(err.Error(), "backup") {
		t.Errorf("error = %q, want mention of backup", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".makecli.tmp")); !os.IsNotExist(statErr) {
		t.Error("stage file leaked on backup failure")
	}
}

// TestInstallBinaryRollsBackOnInstallFailure 验证安装(步骤3)失败、回滚成功时恢复原二进制
func TestInstallBinaryRollsBackOnInstallFailure(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "makecli")
	writeExec(t, exe, "OLD")
	newBin := filepath.Join(t.TempDir(), "new")
	writeExec(t, newBin, "NEW")

	restore := stubRename(2) // 仅第 2 次(安装)失败，第 3 次(回滚)成功
	defer restore()

	err := installBinary(newBin, exe)
	if err == nil {
		t.Fatal("expected install failure, got nil")
	}
	if !strings.Contains(err.Error(), "rolled back") {
		t.Errorf("error = %q, want mention of rolled back", err)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "OLD" {
		t.Errorf("exe content = %q, want OLD (rolled back)", got)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".makecli.tmp")); !os.IsNotExist(statErr) {
		t.Error("stage file leaked on install failure")
	}
}

// TestInstallBinaryRecoveryMessageWhenRollbackFails 验证回滚也失败时给出手动恢复提示，且备份保留
func TestInstallBinaryRecoveryMessageWhenRollbackFails(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "makecli")
	writeExec(t, exe, "OLD")
	newBin := filepath.Join(t.TempDir(), "new")
	writeExec(t, newBin, "NEW")

	restore := stubRename(2, 3) // 安装与回滚都失败
	defer restore()

	err := installBinary(newBin, exe)
	if err == nil {
		t.Fatal("expected install+rollback failure, got nil")
	}
	backupPath := exe + ".old"
	if !strings.Contains(err.Error(), backupPath) {
		t.Errorf("error = %q, want backup path %q for recovery", err, backupPath)
	}
	if !strings.Contains(err.Error(), "mv") {
		t.Errorf("error = %q, want manual recovery hint 'mv'", err)
	}
	got, statErr := os.ReadFile(backupPath)
	if statErr != nil {
		t.Fatalf("backup must be preserved on rollback failure: %v", statErr)
	}
	if string(got) != "OLD" {
		t.Errorf("backup content = %q, want OLD", got)
	}
}
