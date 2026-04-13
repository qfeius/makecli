/**
 * [INPUT]: 依赖 internal/config，bufio、encoding/base64、fmt、os、strings、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newConfigureCmd 函数（含 token/config/set/get 子命令）
 * [POS]: cmd 模块的 configure 命令组，交互式或直接写入 ~/.make/credentials 和 ~/.make/config
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"os"
	"strings"

	"github.com/qfeius/makecli/internal/config"
	"github.com/spf13/cobra"
)

// ---------------------------------- 命令组 ----------------------------------

func newConfigureCmd() *cobra.Command {
	var profile string

	cmd := &cobra.Command{
		Use:          "configure",
		Short:        "Configure MakeCLI credentials and settings",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigureToken(profile)
		},
	}

	cmd.PersistentFlags().StringVar(&profile, "profile", "default", "profile name")
	cmd.AddCommand(newConfigureTokenCmd(&profile))
	cmd.AddCommand(newConfigureConfigCmd(&profile))
	cmd.AddCommand(newConfigureSetCmd(&profile))
	cmd.AddCommand(newConfigureGetCmd(&profile))
	cmd.AddCommand(newConfigureVerifyCmd(&profile))

	return cmd
}

// ---------------------------------- token 子命令 ----------------------------------

func newConfigureTokenCmd(profile *string) *cobra.Command {
	return &cobra.Command{
		Use:          "token",
		Short:        "Configure access token (writes to ~/.make/credentials)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigureToken(*profile)
		},
	}
}

func runConfigureToken(profile string) error {
	creds, err := config.Load()
	if err != nil {
		return err
	}

	current := creds[profile]

	fmt.Printf("Configuring profile [%s]\n", profile)

	token, err := prompt("MakeCLI Access Token", current.AccessToken)
	if err != nil {
		return err
	}
	if token != "" {
		if err := validateJWT(token); err != nil {
			return err
		}
		current.AccessToken = token
	}

	creds[profile] = current
	if err := config.Save(creds); err != nil {
		return err
	}

	path, _ := config.CredentialsPath()
	fmt.Printf("\nCredentials saved to %s\n", path)
	return nil
}

// ---------------------------------- config 子命令 ----------------------------------

func newConfigureConfigCmd(profile *string) *cobra.Command {
	return &cobra.Command{
		Use:          "config",
		Short:        "Configure custom headers (writes to ~/.make/config)",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigureConfig(*profile)
		},
	}
}

func runConfigureConfig(profile string) error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}

	current := cfg[profile]
	fmt.Printf("Configuring config profile [%s]\n", profile)

	serverURL, err := prompt("server-url", current.ServerURL)
	if err != nil {
		return err
	}
	if serverURL != "" {
		current.ServerURL = serverURL
	}

	tenantID, err := prompt("X-Tenant-ID", current.XTenantID)
	if err != nil {
		return err
	}
	if tenantID != "" {
		current.XTenantID = tenantID
	}

	operatorID, err := prompt("X-Operator-ID", current.OperatorID)
	if err != nil {
		return err
	}
	if operatorID != "" {
		current.OperatorID = operatorID
	}

	cfg[profile] = current
	if err := config.SaveConfig(cfg); err != nil {
		return err
	}

	path, _ := config.ConfigPath()
	fmt.Printf("\nConfig saved to %s\n", path)
	return nil
}

// ---------------------------------- set 子命令 ----------------------------------

var validConfigKeys = []string{"server-url", "X-Tenant-ID", "X-Operator-ID"}

func validateConfigKey(key string) error {
	for _, k := range validConfigKeys {
		if key == k {
			return nil
		}
	}
	return fmt.Errorf("unknown config key '%s', valid keys: %s", key, strings.Join(validConfigKeys, ", "))
}

func newConfigureSetCmd(profile *string) *cobra.Command {
	return &cobra.Command{
		Use:          "set <key> <value>",
		Short:        "Set a config value (writes to ~/.make/config)",
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigureSet(*profile, args[0], args[1])
		},
	}
}

func runConfigureSet(profile, key, value string) error {
	if err := validateConfigKey(key); err != nil {
		return err
	}
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	p := cfg[profile]
	switch key {
	case "server-url":
		p.ServerURL = value
	case "X-Tenant-ID":
		p.XTenantID = value
	case "X-Operator-ID":
		p.OperatorID = value
	}
	cfg[profile] = p
	return config.SaveConfig(cfg)
}

// ---------------------------------- get 子命令 ----------------------------------

func newConfigureGetCmd(profile *string) *cobra.Command {
	return &cobra.Command{
		Use:          "get <key>",
		Short:        "Get a config value from ~/.make/config",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigureGet(*profile, args[0])
		},
	}
}

func runConfigureGet(profile, key string) error {
	if err := validateConfigKey(key); err != nil {
		return err
	}
	cfg, err := config.LoadConfig()
	if err != nil {
		return err
	}
	p := cfg[profile]
	switch key {
	case "server-url":
		fmt.Println(p.ServerURL)
	case "X-Tenant-ID":
		fmt.Println(p.XTenantID)
	case "X-Operator-ID":
		fmt.Println(p.OperatorID)
	}
	return nil
}

// ---------------------------------- 共用 helpers ----------------------------------

// prompt 打印提示行（已有值则遮掩末尾4位显示），读取用户输入
// 用户直接回车表示保留当前值，返回空字符串
func prompt(label, current string) (string, error) {
	hint := "None"
	if current != "" {
		hint = mask(current)
	}
	fmt.Printf("%s [%s]: ", label, hint)

	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// mask 保留末尾4位，其余替换为 *
// 短于4位则全部遮掩
func mask(s string) string {
	if len(s) <= 4 {
		return strings.Repeat("*", len(s))
	}
	return strings.Repeat("*", len(s)-4) + s[len(s)-4:]
}

// validateJWT 校验 token 是否符合 JWT 格式（三段 base64url，不验证签名）
func validateJWT(token string) error {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return fmt.Errorf("invalid token format: expected JWT (3 base64url segments separated by '.')")
	}
	for i, part := range parts {
		if _, err := base64.RawURLEncoding.DecodeString(part); err != nil {
			return fmt.Errorf("invalid token format: segment %d is not valid base64url", i+1)
		}
	}
	return nil
}
