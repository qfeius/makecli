/**
 * [INPUT]: 依赖 internal/config、internal/api（Field/CreateEntity）、encoding/json、fmt、os、strings、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newEntityCreateCmd 函数
 * [POS]: cmd/entity 的 create 子命令，校验 fields、调用 Meta Server API 创建 Entity
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/MakeHQ/makecli/internal/api"
	"github.com/MakeHQ/makecli/internal/config"
	"github.com/spf13/cobra"
)

func newEntityCreateCmd() *cobra.Command {
	var profile string
	var server string
	var fieldsFile string

	cmd := &cobra.Command{
		Use:          "create <name>",
		Short:        "Create a new entity on Make",
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			app, _ := cmd.Parent().Flags().GetString("app")
			return runEntityCreate(args[0], app, fieldsFile, profile, server)
		},
	}

	cmd.Flags().StringVar(&fieldsFile, "fields", "", "path to JSON file containing fields array")
	cmd.Flags().StringVar(&profile, "profile", "default", "credentials profile to use")
	cmd.Flags().StringVar(&server, "server", defaultMetaServer, "Meta Server base URL")
	return cmd
}

func runEntityCreate(name, app, fieldsFile, profile, server string) error {
	creds, err := config.Load()
	if err != nil {
		return fmt.Errorf("加载凭证失败: %w", err)
	}

	p, ok := creds[profile]
	if !ok || p.AccessToken == "" {
		return fmt.Errorf("profile '%s' 未配置，请先运行: makecli configure --profile %s", profile, profile)
	}

	fields, err := loadFields(fieldsFile)
	if err != nil {
		return err
	}

	for _, f := range fields {
		if strings.HasPrefix(f.Name, "_") {
			return fmt.Errorf("field 名称不能以 '_' 开头: %q", f.Name)
		}
	}

	if err := api.New(server, p.AccessToken).CreateEntity(name, app, fields); err != nil {
		return err
	}

	fmt.Printf("Entity '%s' created successfully in app '%s'\n", name, app)
	return nil
}

// loadFields 读取 JSON 文件并解析为 []Field；文件路径为空则返回空列表
func loadFields(path string) ([]api.Field, error) {
	if path == "" {
		return []api.Field{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 fields 文件失败: %w", err)
	}
	var fields []api.Field
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, fmt.Errorf("fields 文件格式错误（需为 JSON 数组）: %w", err)
	}
	return fields, nil
}
