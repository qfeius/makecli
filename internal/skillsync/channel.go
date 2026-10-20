/**
 * [INPUT]: 依赖 internal/config 的发布通道常量
 * [OUTPUT]: 对外提供 Branch / SourceURL，将 CLI 通道映射为 Skill 分支与安装来源
 * [POS]: internal/skillsync 的来源单一真相源，安装、同步与远端清单共用
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
