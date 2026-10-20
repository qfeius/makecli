/**
 * [INPUT]: 依赖 internal/config 的发布通道常量
 * [OUTPUT]: 对外提供 Branch / SourceURL；包内 installedChannel 从安装 ref 还原通道
 * [POS]: internal/skillsync 的来源单一真相源，区分目标分支与已安装分支，安装、同步与远端清单共用
 * [PROTOCOL]: 变更时更新此头部，然后检查 AGENTS.md
 */

package skillsync

import "github.com/qfeius/makecli/internal/config"

// Branch 将已校验的通道映射到分支；未设置通道时使用 stable 的 main。
func Branch(channel string) string {
	if channel == config.ChannelBeta {
		return "beta"
	}
	return "main"
}

// SourceURL 显式指定分支，避免默认分支变化影响稳定用户。
func SourceURL(channel string) string {
	return "https://github.com/" + SkillsSource + "/tree/" + Branch(channel)
}

// installedChannel 只信任安装记录；缺少 ref 或自定义 ref 时不推断来源。
func installedChannel(ref string) string {
	switch ref {
	case "main":
		return config.ChannelStable
	case "beta":
		return config.ChannelBeta
	default:
		return StatusUnknown
	}
}
