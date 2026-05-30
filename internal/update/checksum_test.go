/**
 * [INPUT]: 依赖 net/http/httptest、archive/tar、compress/gzip、crypto/sha256、testing；internal/update 自身
 * [OUTPUT]: 单元测试，无导出
 * [POS]: internal/update 校验闸门的测试套件，覆盖 verifyChecksum / fetchChecksums / Apply 的 fail-closed 行为
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// -----------------------------------------------------------------------
// 测试夹具：构造一个含 makecli 二进制的 tar.gz 归档字节流
// -----------------------------------------------------------------------

func makeArchive(t *testing.T, payload []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	hdr := &tar.Header{Name: "makecli", Mode: 0755, Size: int64(len(payload)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("write tar header: %v", err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatalf("write tar body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func writeTempFile(t *testing.T, content []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "archive.tar.gz")
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("write temp archive: %v", err)
	}
	return path
}

// -----------------------------------------------------------------------
// verifyChecksum（纯函数）
// -----------------------------------------------------------------------

func TestVerifyChecksum(t *testing.T) {
	archive := []byte("pretend-this-is-a-binary-archive")
	path := writeTempFile(t, archive)
	good := sha256Hex(archive)
	const fname = "makecli_1.0.0_linux_amd64.tar.gz"

	tests := []struct {
		name      string
		checksums string
		filename  string
		wantErr   bool
	}{
		{
			name:      "correct hash",
			checksums: fmt.Sprintf("%s  %s\n", good, fname),
			filename:  fname,
			wantErr:   false,
		},
		{
			name:      "correct hash among many entries",
			checksums: "deadbeef  other_amd64.tar.gz\n" + fmt.Sprintf("%s  %s\n", good, fname) + "cafef00d  another.tar.gz\n",
			filename:  fname,
			wantErr:   false,
		},
		{
			name:      "uppercase hex still matches",
			checksums: fmt.Sprintf("%s  %s\n", strings.ToUpper(good), fname),
			filename:  fname,
			wantErr:   false,
		},
		{
			name:      "wrong hash",
			checksums: fmt.Sprintf("%s  %s\n", strings.Repeat("0", 64), fname),
			filename:  fname,
			wantErr:   true,
		},
		{
			name:      "filename absent from checksums",
			checksums: fmt.Sprintf("%s  some_other_file.tar.gz\n", good),
			filename:  fname,
			wantErr:   true,
		},
		{
			name:      "empty checksums content",
			checksums: "",
			filename:  fname,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifyChecksum(path, tt.checksums, tt.filename)
			if (err != nil) != tt.wantErr {
				t.Errorf("verifyChecksum error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// -----------------------------------------------------------------------
// fetchChecksums（httptest）
// -----------------------------------------------------------------------

func TestFetchChecksums(t *testing.T) {
	const body = "abc123  makecli_1.0.0_linux_amd64.tar.gz\n"

	t.Run("present", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		defer srv.Close()

		assets := []Asset{{Name: checksumsAssetName, BrowserDownloadURL: srv.URL}}
		got, err := fetchChecksums(assets)
		if err != nil {
			t.Fatalf("fetchChecksums error: %v", err)
		}
		if got != body {
			t.Errorf("fetchChecksums = %q, want %q", got, body)
		}
	})

	t.Run("asset absent fails closed", func(t *testing.T) {
		assets := []Asset{{Name: "makecli_1.0.0_linux_amd64.tar.gz", BrowserDownloadURL: "http://x"}}
		if _, err := fetchChecksums(assets); err == nil {
			t.Error("expected error when checksums.txt asset absent, got nil")
		}
	})

	t.Run("non-200 fails closed", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()

		assets := []Asset{{Name: checksumsAssetName, BrowserDownloadURL: srv.URL}}
		if _, err := fetchChecksums(assets); err == nil {
			t.Error("expected error on HTTP 404, got nil")
		}
	})
}

// -----------------------------------------------------------------------
// Apply 端到端的 fail-closed 行为（httptest 提供归档 + checksums.txt）
//
// 这些用例只验证「不正确 → 不替换二进制」：失败路径在 replaceBinary 之前返回，
// 运行中的测试二进制始终不被触碰。正确 hash 的完整替换不在此测试，
// 因为 replaceBinary 会改写真实文件——校验逻辑已在 verifyChecksum 单测中覆盖。
// -----------------------------------------------------------------------

func TestApplyFailsClosed(t *testing.T) {
	archive := makeArchive(t, []byte("fake-binary-content"))
	goodHash := sha256Hex(archive)
	assetFile := assetName("1.0.0") // 当前平台名，与 Apply 内部 findAsset 一致

	// 路由：/archive 返回归档，/checksums 返回 checksums.txt
	newServer := func(checksumsBody string, serveChecksums bool) *httptest.Server {
		mux := http.NewServeMux()
		mux.HandleFunc("/archive", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write(archive)
		})
		if serveChecksums {
			mux.HandleFunc("/checksums", func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte(checksumsBody))
			})
		}
		return httptest.NewServer(mux)
	}

	tests := []struct {
		name           string
		serveChecksums bool
		checksumsBody  func(base string) string
		includeCkAsset bool
	}{
		{
			name:           "missing checksums asset",
			serveChecksums: false,
			checksumsBody:  func(string) string { return "" },
			includeCkAsset: false,
		},
		{
			name:           "filename missing from checksums",
			serveChecksums: true,
			checksumsBody:  func(string) string { return goodHash + "  unrelated_file.tar.gz\n" },
			includeCkAsset: true,
		},
		{
			name:           "wrong hash",
			serveChecksums: true,
			checksumsBody:  func(base string) string { return strings.Repeat("0", 64) + "  " + base + "\n" },
			includeCkAsset: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newServer(tt.checksumsBody(assetFile), tt.serveChecksums)
			defer srv.Close()

			assets := []Asset{{Name: assetFile, BrowserDownloadURL: srv.URL + "/archive"}}
			if tt.includeCkAsset {
				assets = append(assets, Asset{Name: checksumsAssetName, BrowserDownloadURL: srv.URL + "/checksums"})
			}

			rel := &Release{TagName: "v1.0.0", Assets: assets}
			err := Apply(rel)
			if err == nil {
				t.Fatal("Apply expected error (fail-closed), got nil")
			}
		})
	}
}
