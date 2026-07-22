/**
 * [INPUT]: 依赖 encoding/json、os、path/filepath、strings、time；依赖 internal/config 的 Dir 与 ReplaceFile（平台感知的原子替换）
 * [OUTPUT]: 对外提供（包内）cacheData 类型与 readCache/writeCache/cachePath/cleanStaleTemps，及 expired 方法
 * [POS]: internal/notifier 的本地缓存层，持久化最近一次 GitHub 检测结果，供 Start/Finish 消费
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package notifier

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/qfeius/makecli/internal/config"
)

// cacheData 是 update-check.json 的结构。
// Channel 标记检测结果所属通道：跨通道缓存视为不可用（Start 触发刷新、
// shouldNotify 短路）。旧版二进制写的缓存无此字段 → 空串与任何通道不匹配，
// 触发一次刷新后自愈，无需迁移逻辑。
type cacheData struct {
	CheckedAt     time.Time `json:"checked_at"`
	LatestVersion string    `json:"latest_version"`
	HTMLURL       string    `json:"html_url"`
	Channel       string    `json:"channel"`
}

// cacheFileName 缓存文件名
const cacheFileName = "update-check.json"

// cachePath 返回缓存文件绝对路径（<config.Dir>/update-check.json）
func cachePath() (string, error) {
	dir, err := config.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, cacheFileName), nil
}

// readCache 读取缓存。文件不存在返回零值且无错误；损坏返回零值 + 错误。
func readCache() (cacheData, error) {
	path, err := cachePath()
	if err != nil {
		return cacheData{}, err
	}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return cacheData{}, nil
	}
	if err != nil {
		return cacheData{}, err
	}
	var c cacheData
	if err := json.Unmarshal(b, &c); err != nil {
		return cacheData{}, err
	}
	return c, nil
}

// writeCache 原子写入缓存：写临时文件后 rename，避免与并发读发生撕裂。
func writeCache(c cacheData) error {
	path, err := cachePath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".update-check-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return config.ReplaceFile(tmpName, path)
}

// expired 判定缓存是否已超过 interval（now 显式传入便于测试）
func (c cacheData) expired(interval time.Duration, now time.Time) bool {
	return now.Sub(c.CheckedAt) >= interval
}

// staleTempAge 临时文件被视为孤儿（writeCache 中途夭折残留）的最小年龄。
// 年轻于此阈值的不删，避免误删并发进程正在写入的临时文件。
const staleTempAge = time.Hour

// cleanStaleTemps 清扫 writeCache 留下的孤儿临时文件（.update-check-*.json）。
// best-effort：任何一步出错即静默放弃，绝不影响主流程。真实缓存文件
// update-check.json 无 ".update-check-" 前缀，天然不在清扫范围内。
func cleanStaleTemps(now time.Time) {
	dir, err := config.Dir()
	if err != nil {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, ".update-check-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		info, err := e.Info()
		if err != nil || now.Sub(info.ModTime()) < staleTempAge {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
}
