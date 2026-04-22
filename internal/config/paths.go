/**
 * [INPUT]: 依赖 os、path/filepath
 * [OUTPUT]: 对外提供 EnvConfigDir 常量、Dir 函数
 * [POS]: internal/config 的路径解析中枢，credentials.go/config.go 共享此入口，统一 MAKE_CLI_CONFIG_DIR 覆盖语义
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// EnvConfigDir 覆盖默认 ~/.make 的环境变量名，便于自动化测试与多身份切换
const EnvConfigDir = "MAKE_CLI_CONFIG_DIR"

// Dir 返回配置目录的绝对路径
// 优先级：$MAKE_CLI_CONFIG_DIR > ~/.make
func Dir() (string, error) {
	if d := os.Getenv(EnvConfigDir); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("无法获取 home 目录: %w", err)
	}
	return filepath.Join(home, ".make"), nil
}
