//go:build windows

/**
 * [INPUT]: 依赖 os、time
 * [OUTPUT]: 对外提供 ReplaceFile——Windows 平台的失败安全文件替换（单步覆盖重试，绝不挪动/删除既有目标；导出：internal/notifier 的缓存落盘复用）
 * [POS]: internal/config 落盘原语的平台分支（Windows）：os.Rename 在 Windows 上即 MoveFileEx(MOVEFILE_REPLACE_EXISTING)
 *        单步覆盖原语——成功即原子替换，失败（目标被无 FILE_SHARE_DELETE 的进程占用）则退避重试，任何失败路径
 *        都不触碰既有目标：dst 要么已是新内容、要么原样保有旧内容，不存在路径缺失窗口或旧文件丢失
 *        （曾先删目标再改名，二次改名失败即丢最后完好的配置/缓存）；
 *        POSIX 侧的直通实现见 atomic_replace.go，两者共同支撑 atomic.go 的 atomicWrite 与 internal/notifier 的缓存写入
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package config

import (
	"os"
	"time"
)

// Windows 替换重试参数：并发读取方（如另一个 makecli 进程正在 Load）通常在毫秒级释放句柄，
// 少量短退避即可覆盖；仍失败则把最后一次错误如实上抛，不无限等待。
const (
	replaceRetryCount = 3
	replaceRetryDelay = 10 * time.Millisecond
)

// renameFile 是唯一的替换原语出口，包级变量供失败注入测试打桩——验证「替换失败时
// dst 原样保有旧内容」的合同不依赖真实文件锁场景。
var renameFile = os.Rename

// ReplaceFile 用 src 替换 dst，失败安全：只用 os.Rename 这一个单步覆盖原语
// （Windows 上是 MoveFileEx + MOVEFILE_REPLACE_EXISTING），失败则退避重试。
// 刻意不做「挪走/删除旧目标再放新文件」的回退——那会制造 dst 路径缺失窗口，
// 且第二步失败时旧内容不在原位。这里的合同是：本函数返回错误时，dst 原样保有
// 替换前的内容；代价是目标被独占进程长期打开时替换报错（调用方重试即可），
// 而不是冒丢数据的险去强行成功。
func ReplaceFile(src, dst string) error {
	var err error
	for attempt := 0; attempt < replaceRetryCount; attempt++ {
		if attempt > 0 {
			time.Sleep(replaceRetryDelay)
		}
		if err = renameFile(src, dst); err == nil {
			return nil
		}
	}
	return err
}
