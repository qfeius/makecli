/**
 * [INPUT]: 依赖 archive/zip、bytes、net/http、os、path/filepath、testing；internal/update 自身
 * [OUTPUT]: 单元测试，无导出
 * [POS]: internal/update 的跨平台资产/解压测试，覆盖 Windows zip 与下载客户端超时
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package update

import (
	"archive/zip"
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// TestAssetNameForPlatform 验证资产名随平台切换归档格式：windows→zip，其余→tar.gz
func TestAssetNameForPlatform(t *testing.T) {
	tests := []struct {
		goos, goarch, want string
	}{
		{"linux", "amd64", "makecli_1.2.3_linux_amd64.tar.gz"},
		{"darwin", "arm64", "makecli_1.2.3_darwin_arm64.tar.gz"},
		{"windows", "amd64", "makecli_1.2.3_windows_amd64.zip"},
		{"windows", "arm64", "makecli_1.2.3_windows_arm64.zip"},
	}
	for _, tt := range tests {
		got := assetNameFor("1.2.3", tt.goos, tt.goarch)
		if got != tt.want {
			t.Errorf("assetNameFor(1.2.3, %s, %s) = %q, want %q", tt.goos, tt.goarch, got, tt.want)
		}
	}
}

// TestExtractBinaryFromZip 验证 .zip 归档（Windows）能提取出 makecli.exe 二进制
func TestExtractBinaryFromZip(t *testing.T) {
	payload := []byte("windows-binary-content")

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("makecli.exe")
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	if _, err := w.Write(payload); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}

	archivePath := filepath.Join(t.TempDir(), "makecli_1.0.0_windows_amd64.zip")
	if err := os.WriteFile(archivePath, buf.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}

	binPath, err := extractBinary(archivePath)
	if err != nil {
		t.Fatalf("extractBinary(zip): %v", err)
	}
	defer func() { _ = os.Remove(binPath) }()

	got, err := os.ReadFile(binPath)
	if err != nil {
		t.Fatalf("read extracted: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("extracted content = %q, want %q", got, payload)
	}
}

// TestDownloadClientHasResponseHeaderTimeout 验证大归档下载客户端用 ResponseHeaderTimeout
// 约束「迟迟不响应」，但不设一刀切 Timeout（否则会截断慢但有进展的大文件下载）。
func TestDownloadClientHasResponseHeaderTimeout(t *testing.T) {
	if downloadClient.Timeout != 0 {
		t.Errorf("downloadClient.Timeout = %v, want 0 (hard timeout would truncate large slow downloads)", downloadClient.Timeout)
	}
	tr, ok := downloadClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("downloadClient.Transport is %T, want *http.Transport", downloadClient.Transport)
	}
	if tr.ResponseHeaderTimeout <= 0 {
		t.Error("downloadClient transport must carry a positive ResponseHeaderTimeout to bound a stalled server")
	}
}
