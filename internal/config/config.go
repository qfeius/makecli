/**
 * [INPUT]: 依赖 os、bufio、fmt、io、sort、strings、path/filepath；依赖 paths.go 的 Dir
 * [OUTPUT]: 对外提供 LoadConfig、SaveConfig、ConfigPath 函数，Config/ConfigProfile 类型
 * [POS]: internal/config 的 config 文件管理，读写 config 文件（默认 ~/.make/config，INI 格式）
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package config

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
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

// parseINISections 通用 INI 解析：section → (key → value)。
// 忽略空行与 # / ; 注释；无 section 头的键被丢弃。
func parseINISections(r io.Reader) (map[string]map[string]string, error) {
	sections := map[string]map[string]string{}
	current := ""

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSpace(line[1 : len(line)-1])
			if _, ok := sections[current]; !ok {
				sections[current] = map[string]string{}
			}
			continue
		}
		if current == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		sections[current][strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}
	return sections, scanner.Err()
}

// parseConfigINI 解析 config 文件为 Config（profile 集合），跳过保留的 [settings] 全局段
func parseConfigINI(f *os.File) (Config, error) {
	sections, err := parseINISections(f)
	if err != nil {
		return nil, err
	}
	cfg := Config{}
	for name, kv := range sections {
		if name == settingsSection {
			continue
		}
		cfg[name] = ConfigProfile{
			ServerURL:  kv["server-url"],
			XTenantID:  kv["X-Tenant-ID"],
			OperatorID: kv["X-Operator-ID"],
		}
	}
	return cfg, nil
}

// ---------------------------------- 写入 ----------------------------------

// existingSettings 读取磁盘上已存在的 [settings] 段（Config 模型不含全局段，
// SaveConfig 覆盖写时需显式保留，否则用户手写的全局配置会丢失）。
func existingSettings(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	sections, err := parseINISections(f)
	if err != nil {
		return nil
	}
	return sections[settingsSection]
}

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

	// 覆盖写会清空文件，先抓取磁盘上的 [settings] 全局段以便末尾重新落盘
	settings := existingSettings(path)

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

	// 末尾保留全局 [settings] 段（读路径跳过它，写路径必须显式回写，否则数据丢失）
	if len(settings) > 0 {
		if len(order) > 0 {
			_, _ = fmt.Fprintln(w)
		}
		_, _ = fmt.Fprintf(w, "[%s]\n", settingsSection)
		keys := make([]string, 0, len(settings))
		for k := range settings {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			_, _ = fmt.Fprintf(w, "%s = %s\n", k, settings[k])
		}
	}

	return w.Flush()
}
