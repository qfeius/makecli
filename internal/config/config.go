/**
 * [INPUT]: 依赖 os、bufio、fmt、strings、path/filepath；依赖 paths.go 的 Dir
 * [OUTPUT]: 对外提供 LoadConfig、SaveConfig、ConfigPath 函数，Config/ConfigProfile 类型
 * [POS]: internal/config 的 config 文件管理，读写 config 文件（默认 ~/.make/config，INI 格式）
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ---------------------------------- 数据结构 ----------------------------------

// ConfigProfile 代表一个命名配置块，如 [default]，持有租户与操作者信息
type ConfigProfile struct {
	ServerURL  string
	XTenantID  string
	OperatorID string
}

// Config 是所有 profile 的集合，key 为 profile 名
type Config map[string]ConfigProfile

// ---------------------------------- 路径 ----------------------------------

// ConfigPath 返回 config 文件的绝对路径
// 默认 ~/.make/config，被 $MAKE_CLI_CONFIG_DIR 覆盖
func ConfigPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config"), nil
}

// ---------------------------------- 读取 ----------------------------------

// LoadConfig 从 ~/.make/config 读取所有 profile
// 文件不存在时返回空 Config，不报错
func LoadConfig() (Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return Config{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取 config 失败: %w", err)
	}
	defer func() { _ = f.Close() }()

	return parseConfigINI(f)
}

// parseConfigINI 解析 INI 格式内容，只处理 [section] 和 key = value
func parseConfigINI(f *os.File) (Config, error) {
	cfg := Config{}
	current := ""

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// 跳过空行和注释
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}

		// [section]
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSpace(line[1 : len(line)-1])
			if _, ok := cfg[current]; !ok {
				cfg[current] = ConfigProfile{}
			}
			continue
		}

		// key = value
		if current == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		p := cfg[current]
		switch key {
		case "server-url":
			p.ServerURL = val
		case "X-Tenant-ID":
			p.XTenantID = val
		case "X-Operator-ID":
			p.OperatorID = val
		}
		cfg[current] = p
	}

	return cfg, scanner.Err()
}

// ---------------------------------- 写入 ----------------------------------

// SaveConfig 将 Config 写入 ~/.make/config
// 自动创建 ~/.make/ 目录（0700），文件权限 0600
func SaveConfig(cfg Config) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("创建配置目录 %s 失败: %w", dir, err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("写入 config 失败: %w", err)
	}
	defer func() { _ = f.Close() }()

	// default profile 优先输出，其余保持稳定顺序
	order := []string{}
	if _, ok := cfg["default"]; ok {
		order = append(order, "default")
	}
	for name := range cfg {
		if name != "default" {
			order = append(order, name)
		}
	}

	w := bufio.NewWriter(f)
	for i, name := range order {
		if i > 0 {
			_, _ = fmt.Fprintln(w)
		}
		_, _ = fmt.Fprintf(w, "[%s]\n", name)
		p := cfg[name]
		if p.ServerURL != "" {
			_, _ = fmt.Fprintf(w, "server-url = %s\n", p.ServerURL)
		}
		if p.XTenantID != "" {
			_, _ = fmt.Fprintf(w, "X-Tenant-ID = %s\n", p.XTenantID)
		}
		if p.OperatorID != "" {
			_, _ = fmt.Fprintf(w, "X-Operator-ID = %s\n", p.OperatorID)
		}
	}

	return w.Flush()
}
