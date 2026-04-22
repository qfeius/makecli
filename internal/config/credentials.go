/**
 * [INPUT]: 依赖 os、bufio、fmt、strings、path/filepath；依赖 paths.go 的 Dir
 * [OUTPUT]: 对外提供 Load、Save、CredentialsPath 函数，Credentials/Profile 类型
 * [POS]: internal/config 的核心，管理 credentials 文件（默认 ~/.make/credentials）的 INI 格式读写
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

// Profile 代表一个命名配置块，如 [default] 或 [todo]
type Profile struct {
	AccessToken string
}

// Credentials 是所有 profile 的集合，key 为 profile 名
type Credentials map[string]Profile

// ---------------------------------- 路径 ----------------------------------

// CredentialsPath 返回 credentials 文件的绝对路径
// 默认 ~/.make/credentials，被 $MAKE_CLI_CONFIG_DIR 覆盖
func CredentialsPath() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "credentials"), nil
}

// ---------------------------------- 读取 ----------------------------------

// Load 从 ~/.make/credentials 读取所有 profile
// 文件不存在时返回空 Credentials，不报错
func Load() (Credentials, error) {
	path, err := CredentialsPath()
	if err != nil {
		return nil, err
	}

	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return Credentials{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("读取 credentials 失败: %w", err)
	}
	defer func() { _ = f.Close() }()

	return parseINI(f)
}

// parseINI 解析 INI 格式内容，只处理 [section] 和 key = value
func parseINI(f *os.File) (Credentials, error) {
	creds := Credentials{}
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
			if _, ok := creds[current]; !ok {
				creds[current] = Profile{}
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

		if key == "access_token" {
			p := creds[current]
			p.AccessToken = val
			creds[current] = p
		}
	}

	return creds, scanner.Err()
}

// ---------------------------------- 写入 ----------------------------------

// Save 将 Credentials 写入 ~/.make/credentials
// 自动创建 ~/.make/ 目录（0700），文件权限 0600
func Save(creds Credentials) error {
	path, err := CredentialsPath()
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("创建配置目录 %s 失败: %w", dir, err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("写入 credentials 失败: %w", err)
	}
	defer func() { _ = f.Close() }()

	// default profile 优先输出，其余按字典序
	order := []string{}
	if _, ok := creds["default"]; ok {
		order = append(order, "default")
	}
	for name := range creds {
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
		_, _ = fmt.Fprintf(w, "access_token = %s\n", creds[name].AccessToken)
	}

	return w.Flush()
}
