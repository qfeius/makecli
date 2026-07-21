/**
 * [INPUT]: 依赖 fmt、os、strconv；依赖 config.go 的 parseINISections、ConfigPath
 * [OUTPUT]: 对外提供 Settings 类型、LoadSettings、ValidateProfileName 函数；包内 settingsSection 常量
 * [POS]: internal/config 的全局设置读取，承载非 profile 相关的 [settings] 段（check-for-updates / environment / channel）
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package config

import (
	"fmt"
	"os"
	"strconv"
)

// settingsSection 是 config 文件中承载全局（非 profile）配置的保留段名
const settingsSection = "settings"

// ValidateProfileName 拒绝把保留段名 settings 用作 profile 名。否则写出的 [settings]
// profile 段会与全局 [settings] 段在同一 INI 文件里碰撞——读时被 profile 解析层跳过、
// 数据静默丢失。profile 与 settings 共用 INI section 命名空间，故此名必须独占。
func ValidateProfileName(name string) error {
	if name == settingsSection {
		return fmt.Errorf("%q is a reserved section name and cannot be used as a profile", name)
	}
	return nil
}

// Settings 持有全局配置项。指针字段表达三态：nil = 文件未设置该项。
type Settings struct {
	// CheckForUpdates 控制自动更新提示是否启用；nil 表示未配置（由调用方决定默认）
	CheckForUpdates *bool
	// Environment 是当前后端环境名（dev/test/production）；空串 = 未配置（调用方回退 DefaultEnvironment）
	Environment string
	// Channel 是发布通道名（stable/beta）；空串 = 未配置（调用方回退 DefaultChannel）
	Channel string
}

// LoadSettings 读取 config 文件的 [settings] 全局段。
// best-effort：文件不存在返回空 Settings 且无错误；解析失败返回错误。
func LoadSettings() (Settings, error) {
	path, err := ConfigPath()
	if err != nil {
		return Settings{}, err
	}

	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return Settings{}, nil
	}
	if err != nil {
		return Settings{}, fmt.Errorf("读取 config 失败: %w", err)
	}
	defer func() { _ = f.Close() }()

	sections, err := parseINISections(f)
	if err != nil {
		return Settings{}, err
	}

	var s Settings
	if kv, ok := sections[settingsSection]; ok {
		if raw, ok := kv["check-for-updates"]; ok {
			if b, err := strconv.ParseBool(raw); err == nil {
				s.CheckForUpdates = &b
			}
		}
		s.Environment = kv["environment"]
		s.Channel = kv["channel"]
	}
	return s, nil
}
