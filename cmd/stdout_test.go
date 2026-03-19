/**
 * [INPUT]: 依赖 os、bytes、io、testing
 * [OUTPUT]: 对外提供 captureStdout 测试辅助函数，劫持 os.Stdout 捕获输出
 * [POS]: cmd 模块的测试基础设施，被各子命令测试文件复用
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"bytes"
	"io"
	"os"
	"testing"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	originalStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}

	os.Stdout = writer

	outputC := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, reader)
		outputC <- buf.String()
	}()

	fn()

	_ = writer.Close()
	os.Stdout = originalStdout
	output := <-outputC
	_ = reader.Close()

	return output
}
