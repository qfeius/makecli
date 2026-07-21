/**
 * [INPUT]: 依赖 os、time；依赖 github.com/mattn/go-isatty、internal/build 的 Version、internal/config 的 LoadSettings、internal/update 的 CheckLatest
 * [OUTPUT]: 对外提供 Notifier 类型、Start、(*Notifier).Finish；包内 isStderrTTY 钩子
 * [POS]: internal/notifier 的编排入口，被 cmd.Execute 在命令头尾钩入：按通道并行刷新缓存（失败也落盘退避标记 + 清扫孤儿 temp）+ 收尾打印提示；channelOf 对未知通道值 fail-safe 回退 stable
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package notifier

import (
	"os"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/qfeius/makecli/internal/build"
	"github.com/qfeius/makecli/internal/config"
	"github.com/qfeius/makecli/internal/update"
)

const (
	checkInterval  = 24 * time.Hour
	finishDeadline = 250 * time.Millisecond
	envEnable      = "MAKE_CLI_UPDATE_NOTIFIER"
)

// isStderrTTY 检测 stderr 是否为终端；包级变量便于测试替换
var isStderrTTY = func() bool {
	return isatty.IsTerminal(os.Stderr.Fd())
}

// Notifier 协调后台刷新与收尾提示
type Notifier struct {
	done chan struct{}
}

// channelOf 从 Settings 提取通道，未知值回退 stable——notifier 侧 fail-safe
// 不报错（报错属于 update 命令的职责边界，见 cmd resolveChannel）。
func channelOf(s config.Settings) string {
	if s.Channel == config.ChannelBeta {
		return config.ChannelBeta
	}
	return config.ChannelStable
}

// Start 读缓存；缓存过期或跨通道才起后台 goroutine 刷新。立即返回，不阻塞主命令。
func Start() *Notifier {
	n := &Notifier{done: make(chan struct{})}

	settings, _ := config.LoadSettings()
	channel := channelOf(settings)

	cache, _ := readCache()
	if !cache.expired(checkInterval, time.Now()) && cache.Channel == channel {
		close(n.done)
		return n
	}

	go func() {
		defer close(n.done)
		defer func() { _ = recover() }() // 兜底 panic，绝不影响主流程

		cleanStaleTemps(time.Now()) // 清扫此前 writeCache 夭折的孤儿临时文件

		release, _, err := update.CheckLatest(build.Version, channel == config.ChannelBeta)
		if err != nil || release == nil {
			// 刷新失败也落盘退避标记：CheckedAt 前进、版本留空（shouldNotify
			// 已对空版本短路）、通道记当前（避免下次误判跨通道再刷）。否则慢/
			// 离线机器每次命令都重新 spawn 并再付一次 finishDeadline 等待。
			_ = writeCache(cacheData{CheckedAt: time.Now(), Channel: channel})
			return
		}
		_ = writeCache(cacheData{
			CheckedAt:     time.Now(),
			LatestVersion: release.TagName,
			HTMLURL:       release.HTMLURL,
			Channel:       channel,
		})
	}()
	return n
}

// Finish 给后台刷新一个极短的收尾窗口，然后按判定链决定是否打印提示。
// cmdName 为本次调用的顶级命令名（由 cmd 层解析传入）。
func (n *Notifier) Finish(cmdName string) {
	select {
	case <-n.done:
	case <-time.After(finishDeadline):
	}

	settings, _ := config.LoadSettings()
	if !notifierEnabled(os.Getenv(envEnable), settings.CheckForUpdates) {
		return
	}

	cache, err := readCache()
	if err != nil {
		return
	}
	if !shouldNotify(build.Version, cmdName, isStderrTTY(), os.Getenv("CI"), cache, channelOf(settings)) {
		return
	}
	renderNotice(os.Stderr, build.Version, cache)
}
