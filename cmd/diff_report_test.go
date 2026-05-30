/**
 * [INPUT]: 依赖 bytes、errors、os、path/filepath、testing；依赖 github.com/spf13/cobra
 * [OUTPUT]: 覆盖 reportDiffError 单元行为 + diff 命令真实错误打印的集成行为
 * [POS]: cmd 模块 diff 子命令的错误上报测试，守护 SilenceErrors 下真实错误不被吞掉
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
)

// TestReportDiffError 锁定契约：diff 命令开启 SilenceErrors 后，真实错误必须由
// reportDiffError 自行打印到 stderr，而 errDiffFound 哨兵（仅驱动退出码）须保持静默。
func TestReportDiffError(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantPrint bool
		wantSame  bool // 返回值是否原样透传 err
	}{
		{"real error prints", errors.New("获取远端 Entity 失败: connection refused"), true, true},
		{"sentinel stays silent", errDiffFound, false, true},
		{"nil stays silent", nil, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			var buf bytes.Buffer
			cmd.SetErr(&buf)

			got := reportDiffError(cmd, tt.err)

			if (buf.Len() > 0) != tt.wantPrint {
				t.Errorf("printed=%v (%q), want printed=%v", buf.Len() > 0, buf.String(), tt.wantPrint)
			}
			if tt.wantSame && got != tt.err {
				t.Errorf("reportDiffError returned %v, want original %v", got, tt.err)
			}
		})
	}
}

// TestDiffCommandPrintsRealError 端到端守护回归：通过 cobra 执行 diff 命令，喂入一个
// 必然让 loadLocalForDiff 解析失败的非法 YAML 文件，断言真实错误确实被打印到命令的
// 错误输出（SilenceErrors 下若无 reportDiffError，此处会静默——正是被修复的回归）。
func TestDiffCommandPrintsRealError(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.yaml")
	if err := os.WriteFile(bad, []byte("[unterminated-flow"), 0644); err != nil {
		t.Fatal(err)
	}

	cmd := newDiffCmd()
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetArgs([]string{"-f", bad})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected a real error for invalid YAML, got nil")
	}
	if errors.Is(err, errDiffFound) {
		t.Fatalf("invalid YAML must be a real error, not the diff sentinel: %v", err)
	}
	if errBuf.Len() == 0 {
		t.Error("real diff error must be printed to stderr; got nothing (SilenceErrors regression)")
	}
}
