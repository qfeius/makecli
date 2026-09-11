/**
 * [INPUT]: 依赖 os、time；依赖 github.com/mattn/go-isatty、internal/build 的 Version、internal/config 的 LoadSettings、internal/update 的 CheckLatest
 * [OUTPUT]: 对外提供 Notifier 类型、Start(cmdName)、(*Notifier).Finish；包内 isStderrTTY 钩子
 * [POS]: internal/notifier 的编排入口，被 cmd.Execute 在命令头尾钩入：Start 读缓存过判定链写 pending（零网络）、缓存过期或跨通道再起 goroutine 按通道刷新并重写 pending（失败也落盘退避标记 + 清扫孤儿 temp）；Finish 收尾把 pending 渲染到 stderr（仅 TTY）；channelOf 对未知通道值 fail-safe 回退 stable
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

// Start 读缓存过判定链写入 pending（零网络，立即返回）；缓存过期或跨通道才起后台
// goroutine 刷新，刷新落盘后再过一次判定链重写 pending。不阻塞主命令。
// cmdName 为本次调用的顶级命令名（由 cmd 层解析传入），skipCommands 据此抑制。
func Start(cmdName string) *Notifier {
	n := &Notifier{done: make(chan struct{})}
	pending.Store(nil)

	settings, _ := config.LoadSettings()
	channel := channelOf(settings)
	enabled := notifierEnabled(os.Getenv(envEnable), settings.CheckForUpdates)
	ci := os.Getenv("CI")
	// publish 把一份缓存快照过判定链后写入 pending；启动时与刷新后各调一次
	publish := func(c cacheData) {
		if enabled {
			pending.Store(pendingUpdate(build.Version, cmdName, ci, c, channel))
		}
	}

	cache, _ := readCache()
	publish(cache)
	if !cache.expired(checkInterval, time.Now()) && cache.Channel == channel {
		close(n.done)
		return n
	}

	go func() {
		defer close(n.done)
		defer func() { _ = recover() }() // 兜底 panic，绝不影响主流程

		cleanStaleTemps(time.Now()) // 清扫此前 writeCache 夭折的孤儿临时文件

		// 刷新失败也落盘退避标记：CheckedAt 前进、版本留空（pendingUpdate
		// 已对空版本短路）、通道记当前（避免下次误判跨通道再刷）。否则慢/
		// 离线机器每次命令都重新 spawn 并再付一次 finishDeadline 等待。
		fresh := cacheData{CheckedAt: time.Now(), Channel: channel}
		if release, _, err := update.CheckLatest(build.Version, channel == config.ChannelBeta); err == nil && release != nil {
			fresh.LatestVersion = release.TagName
			fresh.HTMLURL = release.HTMLURL
		}
		_ = writeCache(fresh)
		publish(fresh)
	}()
	return n
}

// Finish 给后台刷新一个极短的收尾窗口，然后把待提示更新渲染到 stderr。
// 仅 TTY 渲染：非 TTY 的 agent 从 JSON 出口的 _notice 拿同一份数据（cmd/output writeJSON）。
func (n *Notifier) Finish() {
	select {
	case <-n.done:
	case <-time.After(finishDeadline):
	}
	if u := Pending(); u != nil && isStderrTTY() {
		renderNotice(os.Stderr, u)
	}
}
