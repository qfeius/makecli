/**
 * [INPUT]: 依赖 crypto/sha256、encoding/hex、bufio、net/http、os、time；与 update.go 同包共享 Asset
 * [OUTPUT]: 对外（包内）提供 verifyChecksum 纯函数、fetchChecksums 拉取步骤；供 Apply 在替换二进制前做完整性校验
 * [POS]: internal/update 的供应链安全闸门，挡在 download 与 replaceBinary 之间，fail-closed
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package update

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// -----------------------------------------------------------------------
// 校验和文件名（GoReleaser 默认产出）
// -----------------------------------------------------------------------

const checksumsAssetName = "checksums.txt"

// checksumsClient 仅用于拉取体积极小的 checksums.txt，给短超时。
// 大归档下载走 download() 的无超时客户端，避免大文件被短超时打断。
var checksumsClient = &http.Client{Timeout: 10 * time.Second}

// -----------------------------------------------------------------------
// 校验逻辑（纯函数，可独立单测，不触碰真实二进制）
// -----------------------------------------------------------------------

// verifyChecksum 校验 archivePath 文件的 SHA-256 是否匹配 checksumsContent 中
// archiveFilename 对应的期望值。
//
// checksumsContent 为 GoReleaser 默认格式，每行一条：
//
//	<sha256-hex><两个空格><文件名>
//
// fail-closed 原则：文件名缺失、计算失败、哈希不符，一律返回 error。
func verifyChecksum(archivePath, checksumsContent, archiveFilename string) error {
	expected, ok := parseChecksums(checksumsContent)[archiveFilename]
	if !ok {
		return fmt.Errorf("no checksum entry for %q in checksums.txt", archiveFilename)
	}

	actual, err := sha256File(archivePath)
	if err != nil {
		return fmt.Errorf("failed to compute checksum: %w", err)
	}

	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", archiveFilename, expected, actual)
	}
	return nil
}

// parseChecksums 把 checksums.txt 内容解析成 文件名→hex 映射。
// 容错：跳过空行与格式异常行（不足两段的行）。
func parseChecksums(content string) map[string]string {
	out := make(map[string]string)
	sc := bufio.NewScanner(strings.NewReader(content))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) != 2 {
			continue
		}
		out[fields[1]] = fields[0]
	}
	return out
}

// sha256File 计算文件的 SHA-256，返回小写 hex。
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// -----------------------------------------------------------------------
// 拉取步骤
// -----------------------------------------------------------------------

// fetchChecksums 下载 release 中的 checksums.txt 资产内容。
// 资产缺失（fail-closed）、网络错误、非 200 一律返回 error。
// 校验和文件体积极小，走短超时的 checksumsClient。
func fetchChecksums(assets []Asset) (string, error) {
	asset := findChecksumsAsset(assets)
	if asset == nil {
		return "", fmt.Errorf("release missing %s; refusing to update without integrity verification", checksumsAssetName)
	}

	resp, err := checksumsClient.Get(asset.BrowserDownloadURL)
	if err != nil {
		return "", fmt.Errorf("failed to download %s: %w", checksumsAssetName, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download %s: HTTP %d", checksumsAssetName, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read %s: %w", checksumsAssetName, err)
	}
	return string(body), nil
}

// findChecksumsAsset 在 assets 中按精确名匹配 checksums.txt。
func findChecksumsAsset(assets []Asset) *Asset {
	for i := range assets {
		if assets[i].Name == checksumsAssetName {
			return &assets[i]
		}
	}
	return nil
}
