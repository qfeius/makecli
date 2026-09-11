/**
 * [INPUT]: 依赖 fmt、strings、sync/atomic
 * [OUTPUT]: 对外提供 Update 类型（_notice.update 的 wire 形状）、Pending 读取器、SetPendingForTest 测试钩子；包内 newUpdate 构造器与 pending 单一真相源
 * [POS]: internal/notifier 的对外契约层——进程级「待提示更新」快照，被 notifier.go 的 Start 写入（启动读缓存 + 后台刷新后各一次），被 Finish（stderr，仅 TTY）与 cmd/output writeJSON（_notice，不问 TTY）两个消费者读取
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package notifier

import (
	"fmt"
	"strings"
	"sync/atomic"
)

// Update 是一条待提示的更新，同时是 JSON 输出里 _notice.update 的 wire 形状。
// current/latest 给程序比较，command 给能执行工具的 agent 直接跑，message 给只读一句话的弱 agent：
// 三种消费者一份数据，字段刻意冗余。
type Update struct {
	Current string `json:"current"`
	Latest  string `json:"latest"`
	URL     string `json:"url,omitempty"`
	Command string `json:"command"`
	Message string `json:"message"`
}

const updateCommand = "makecli update"

// newUpdate 由当前版本与缓存中的最新版本构造 Update；版本号统一去掉 v 前缀展示。
func newUpdate(current, latest, url string) *Update {
	cur := strings.TrimPrefix(current, "v")
	lat := strings.TrimPrefix(latest, "v")
	return &Update{
		Current: cur,
		Latest:  lat,
		URL:     url,
		Command: updateCommand,
		Message: fmt.Sprintf("makecli %s available, current %s, run: %s", lat, cur, updateCommand),
	}
}

// pending 是进程级待提示更新的单一真相源：nil = 无更新，或已被判定链抑制。
var pending atomic.Pointer[Update]

// Pending 返回当前待提示的更新快照；nil 表示无。任何 JSON 出口可随时读取，零阻塞。
func Pending() *Update { return pending.Load() }

// SetPendingForTest 直接写入 pending，供跨包测试构造「有更新」场景；返回旧值便于还原。
func SetPendingForTest(u *Update) *Update { return pending.Swap(u) }
