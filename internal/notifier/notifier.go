/**
 * [INPUT]: 依赖 os、time；依赖 github.com/mattn/go-isatty、internal/build 的 Version、internal/config 的 LoadSettings、internal/update 的 CheckLatest
 * [OUTPUT]: 对外提供 Notifier 类型、Start、(*Notifier).Finish；包内 isStderrTTY 钩子
 * [POS]: internal/notifier 的编排入口，被 cmd.Execute 在命令头尾钩入：并行刷新缓存 + 收尾打印提示
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

// Start 读缓存；缓存过期才起后台 goroutine 刷新。立即返回，不阻塞主命令。
func Start() *Notifier {
	n := &Notifier{done: make(chan struct{})}

	cache, _ := readCache()
	if !cache.expired(checkInterval, time.Now()) {
		close(n.done)
		return n
	}

	go func() {
		defer close(n.done)
		defer func() { _ = recover() }() // 兜底 panic，绝不影响主流程

		release, _, err := update.CheckLatest(build.Version)
		if err != nil || release == nil {
			return
		}
		_ = writeCache(cacheData{
			CheckedAt:     time.Now(),
			LatestVersion: release.TagName,
			HTMLURL:       release.HTMLURL,
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
	if !shouldNotify(build.Version, cmdName, isStderrTTY(), os.Getenv("CI"), cache) {
		return
	}
	renderNotice(os.Stderr, build.Version, cache)
}
