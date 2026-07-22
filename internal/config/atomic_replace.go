//go:build !windows

/**
 * [INPUT]: 依赖 os
 * [OUTPUT]: 对外提供 ReplaceFile——非 Windows 平台的原子文件替换（导出：internal/notifier 的缓存落盘复用）
 * [POS]: internal/config 落盘原语的平台分支（POSIX）：rename(2) 对既有目标本就是原子覆盖，直接透传 os.Rename；
 *        Windows 侧的单步覆盖重试兄弟实现见 atomic_replace_windows.go，两者共同支撑 atomic.go 的 atomicWrite 与 internal/notifier 的缓存写入
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package config

import "os"

// ReplaceFile 用 src 原子替换 dst。POSIX rename(2) 对既有目标是原子覆盖，无需额外处理。
func ReplaceFile(src, dst string) error {
	return os.Rename(src, dst)
}
