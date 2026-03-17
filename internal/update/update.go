/**
 * [INPUT]: 依赖 net/http、archive/tar、compress/gzip、encoding/json、github.com/Masterminds/semver/v3
 * [OUTPUT]: 对外提供 CheckLatest / Apply 函数、Release / Asset 结构体
 * [POS]: internal/update 的核心引擎，被 cmd/update.go 消费
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package update

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// -----------------------------------------------------------------------
// 数据结构
// -----------------------------------------------------------------------

// Release 表示 GitHub Releases API 返回的最新版本
type Release struct {
	TagName string  `json:"tag_name"`
	Assets  []Asset `json:"assets"`
}

// Asset 表示 release 中的单个可下载文件
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// -----------------------------------------------------------------------
// GitHub API 基础 URL（可在测试中替换）
// -----------------------------------------------------------------------

var apiBaseURL = "https://api.github.com"

// -----------------------------------------------------------------------
// 公开 API
// -----------------------------------------------------------------------

// CheckLatest 查询 GitHub 最新 release，返回 release 信息和是否有更新
func CheckLatest(currentVersion string) (*Release, bool, error) {
	url := apiBaseURL + "/repos/MakeHQ/makecli/releases/latest"

	resp, err := http.Get(url)
	if err != nil {
		return nil, false, fmt.Errorf("failed to check for updates: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("failed to check for updates: HTTP %d", resp.StatusCode)
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, false, fmt.Errorf("failed to parse release info: %w", err)
	}

	newer := isNewer(currentVersion, release.TagName)
	return &release, newer, nil
}

// Apply 下载指定 release 的 asset 并替换当前二进制
func Apply(release *Release) error {
	version := strings.TrimPrefix(release.TagName, "v")

	asset, err := findAsset(release.Assets, version)
	if err != nil {
		return err
	}

	// 下载 tar.gz
	archivePath, err := download(asset.BrowserDownloadURL)
	if err != nil {
		return err
	}
	defer os.Remove(archivePath)

	// 从归档中提取二进制
	newBinaryPath, err := extractBinary(archivePath)
	if err != nil {
		return err
	}
	defer os.Remove(newBinaryPath)

	return replaceBinary(newBinaryPath)
}

// -----------------------------------------------------------------------
// 内部实现
// -----------------------------------------------------------------------

// assetName 拼接当前平台对应的 asset 文件名
func assetName(version string) string {
	return fmt.Sprintf("makecli_%s_%s_%s.tar.gz", version, runtime.GOOS, runtime.GOARCH)
}

// findAsset 从 assets 列表中匹配当前平台
func findAsset(assets []Asset, version string) (*Asset, error) {
	target := assetName(version)
	for i := range assets {
		if assets[i].Name == target {
			return &assets[i], nil
		}
	}
	return nil, fmt.Errorf("no release available for %s/%s", runtime.GOOS, runtime.GOARCH)
}

// download 下载 URL 内容到临时文件，返回临时文件路径
func download(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("failed to download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download: HTTP %d", resp.StatusCode)
	}

	tmp, err := os.CreateTemp("", "makecli-update-*.tar.gz")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tmp.Close()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("failed to save download: %w", err)
	}

	return tmp.Name(), nil
}

// extractBinary 从 tar.gz 归档中提取 makecli 二进制到临时文件
func extractBinary(archivePath string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("failed to decompress: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("failed to read archive: %w", err)
		}

		// 匹配 "makecli" 文件（忽略目录前缀）
		base := filepath.Base(hdr.Name)
		if base != "makecli" || hdr.Typeflag != tar.TypeReg {
			continue
		}

		tmp, err := os.CreateTemp("", "makecli-bin-*")
		if err != nil {
			return "", err
		}

		if _, err := io.Copy(tmp, tr); err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
			return "", fmt.Errorf("failed to extract binary: %w", err)
		}
		tmp.Close()

		if err := os.Chmod(tmp.Name(), 0755); err != nil {
			os.Remove(tmp.Name())
			return "", err
		}

		return tmp.Name(), nil
	}

	return "", fmt.Errorf("makecli binary not found in archive")
}

// replaceBinary 原子替换当前运行的二进制
//
// 步骤:
//  1. copy 新二进制到目标目录（确保同一文件系统）
//  2. rename current → current.old（备份）
//  3. rename tmp → current（安装）
//  4. 删除 current.old（清理，best-effort）
//  5. 若步骤 3 失败，回滚 current.old → current
func replaceBinary(newBinaryPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to locate current binary: %w", err)
	}

	// 解析符号链接（Homebrew 场景）
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return fmt.Errorf("failed to resolve symlink: %w", err)
	}

	dir := filepath.Dir(exe)

	// 检查目录写权限
	if err := checkWritable(dir); err != nil {
		return fmt.Errorf("permission denied, try: sudo makecli update")
	}

	// 步骤 1: copy 到同一目录确保同一文件系统
	stagePath := filepath.Join(dir, ".makecli.tmp")
	if err := copyFile(newBinaryPath, stagePath); err != nil {
		return fmt.Errorf("failed to stage new binary: %w", err)
	}

	backupPath := exe + ".old"

	// 步骤 2: 备份当前二进制
	if err := os.Rename(exe, backupPath); err != nil {
		os.Remove(stagePath)
		return fmt.Errorf("failed to backup current binary: %w", err)
	}

	// 步骤 3: 安装新二进制
	if err := os.Rename(stagePath, exe); err != nil {
		// 回滚
		_ = os.Rename(backupPath, exe)
		return fmt.Errorf("failed to install new binary: %w", err)
	}

	// 步骤 4: 清理备份（best-effort）
	os.Remove(backupPath)

	return nil
}

// checkWritable 检查目录是否可写
func checkWritable(dir string) error {
	tmp := filepath.Join(dir, ".makecli-write-test")
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	f.Close()
	os.Remove(tmp)
	return nil
}

// copyFile 将 src 复制到 dst，保留可执行权限
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// isNewer 使用 semver 比较版本，DEV 版本视为始终可更新
func isNewer(current, remote string) bool {
	remote = strings.TrimPrefix(remote, "v")
	current = strings.TrimPrefix(current, "v")

	cur, err := semver.NewVersion(current)
	if err != nil {
		// DEV 或非法版本，视为始终可更新
		return true
	}

	rem, err := semver.NewVersion(remote)
	if err != nil {
		return false
	}

	return rem.GreaterThan(cur)
}
