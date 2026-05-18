/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、cmd/app（loadAppManifestFromFile、validResourceKey）、fmt、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newAppCreateCmd 函数
 * [POS]: cmd/app 的 create 子命令，调用 Meta Server API 创建 App。位置参数是 App key（英文标识符），--name 为展示名（必填，支持中文）；支持 --description 和 -f 文件模式
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newAppCreateCmd() *cobra.Command {
	var description string
	var displayName string
	var file string

	cmd := &cobra.Command{
		Use:   "create [key]",
		Short: "Create a new app on Make",
		Example: `  makecli app create myapp
  makecli app create myapp --name "我的应用"
  makecli app create myapp --name "My App" --description "my awesome app"
  makecli app create -f app.yaml`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if file != "" {
				return runAppCreateFromFile(file)
			}
			if len(args) == 0 {
				return fmt.Errorf("requires app key or -f flag")
			}
			return runAppCreate(args[0], displayName, description)
		},
	}

	cmd.Flags().StringVar(&description, "description", "", "app description")
	cmd.Flags().StringVar(&displayName, "name", "", "app display name (defaults to key)")
	cmd.Flags().StringVarP(&file, "file", "f", "", "path to YAML file containing Make.App resource")
	return cmd
}

func runAppCreateFromFile(path string) error {
	manifest, err := loadAppManifestFromFile(path)
	if err != nil {
		return err
	}

	if err := validResourceKey(manifest.Key); err != nil {
		return err
	}

	client, err := newClientFromProfile()
	if err != nil {
		return err
	}

	props := manifest.Properties
	if props == nil {
		props = map[string]any{}
	}

	// 展示名缺省时回退用 key
	displayName := manifest.Name
	if displayName == "" {
		displayName = manifest.Key
	}

	if apiErr := client.CreateApp(manifest.Key, displayName, props); apiErr != nil {
		return apiErr
	}

	fmt.Printf("App '%s' created successfully\n", manifest.Key)
	return nil
}

func runAppCreate(key, displayName, description string) error {
	if err := validResourceKey(key); err != nil {
		return err
	}

	client, err := newClientFromProfile()
	if err != nil {
		return err
	}

	// 展示名缺省时回退用 key
	if displayName == "" {
		displayName = key
	}

	props := map[string]any{}
	if description != "" {
		props["description"] = description
	}

	if apiErr := client.CreateApp(key, displayName, props); apiErr != nil {
		return apiErr
	}

	fmt.Printf("App '%s' created successfully\n", key)
	return nil
}
