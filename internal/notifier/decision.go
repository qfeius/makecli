/**
 * [INPUT]: 依赖 fmt、io、strconv、strings；依赖 github.com/Masterminds/semver/v3、internal/update 的 CompareVersions
 * [OUTPUT]: 对外提供（包内）notifierEnabled / shouldNotify / renderNotice 与 skipCommands 表
 * [POS]: internal/notifier 的判定与渲染层，被 notifier.go 的 Finish 编排
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package notifier

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/qfeius/makecli/internal/update"
)

// skipCommands 列出不应触发更新提示的顶级命令
var skipCommands = map[string]bool{
	"version":    true,
	"update":     true,
	"help":       true,
	"completion": true,
}

// notifierEnabled 三态裁决是否启用更新提示：env > config > 默认(true)。
//
//	envVal: MAKE_CLI_UPDATE_NOTIFIER 原始值（先 TrimSpace；空/纯空白 = 未设置；非法值忽略并下沉）
//	cfgVal: config [settings] check-for-updates（nil = 未设置）
func notifierEnabled(envVal string, cfgVal *bool) bool {
	if v := strings.TrimSpace(envVal); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	if cfgVal != nil {
		return *cfgVal
	}
	return true
}

// isReleaseVersion 判定 current 是否为正式发布版本（无 prerelease 段）。
// DEV / 非法 semver → false；带 prerelease 的版本（含 git-describe 伪版本
// 如 v0.3.0-16-ga4765c1，以及 go install 的模块伪版本）一律视为开发态 → false，
// 否则会因 semver「prerelease 低于正式版」把降级误报成升级。
func isReleaseVersion(current string) bool {
	v, err := semver.NewVersion(strings.TrimPrefix(current, "v"))
	if err != nil {
		return false
	}
	return v.Prerelease() == ""
}

// shouldNotify 在「已启用」前提下，判定是否真的要打印提示。任一条件不满足即 false。
//
// 顺序：isReleaseVersion 必须在 CompareVersions 之前，因为对 DEV/非法版本
// CompareVersions 恒返回 +1，不加此守卫会让开发构建永远显示更新提示。
func shouldNotify(current, cmdName string, isTTY bool, ci string, cache cacheData) bool {
	if !isReleaseVersion(current) {
		return false
	}
	if ci != "" {
		return false
	}
	if !isTTY {
		return false
	}
	if cmdName == "" || skipCommands[cmdName] {
		return false
	}
	if cache.LatestVersion == "" {
		return false
	}
	return update.CompareVersions(cache.LatestVersion, current) > 0
}

// renderNotice 将升级提示写入 w（调用方传 os.Stderr）
func renderNotice(w io.Writer, current string, cache cacheData) {
	cur := strings.TrimPrefix(current, "v")
	latest := strings.TrimPrefix(cache.LatestVersion, "v")
	const line = "─────────────────────────────────────────────"
	_, _ = fmt.Fprintf(w, "\n%s\n", line)
	_, _ = fmt.Fprintf(w, " A new release of makecli is available: %s → %s\n", cur, latest)
	_, _ = fmt.Fprintf(w, " To upgrade, run: makecli update\n")
	if cache.HTMLURL != "" {
		_, _ = fmt.Fprintf(w, " %s\n", cache.HTMLURL)
	}
	_, _ = fmt.Fprintf(w, "%s\n", line)
}
