/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、cmd/app（loadAppManifestFromFile）、fmt、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newAppCreateCmd 函数
 * [POS]: cmd/app 的 create 子命令，调用 Meta Server API 创建 App，支持 --description / --render-name 选项和 -f 文件模式
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newAppCreateCmd() *cobra.Command {
	var profile string
	var description string
	var renderName string
	var file string

	cmd := &cobra.Command{
		Use:   "create [name]",
		Short: "Create a new app on Make",
		Example: `  makecli app create myapp
  makecli app create myapp --description "my awesome app"
  makecli app create myapp --render-name "My App"
  makecli app create -f app.yaml`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if file != "" {
				return runAppCreateFromFile(file, profile)
			}
			if len(args) == 0 {
				return fmt.Errorf("requires app name or -f flag")
			}
			return runAppCreate(args[0], description, renderName, profile)
		},
	}

	cmd.Flags().StringVar(&profile, "profile", "default", "credentials profile to use")
	cmd.Flags().StringVar(&description, "description", "", "app description")
	cmd.Flags().StringVar(&renderName, "render-name", "", "app display name (defaults to name)")
	cmd.Flags().StringVarP(&file, "file", "f", "", "path to YAML file containing Make.App resource")
	return cmd
}

func runAppCreateFromFile(path, profile string) error {
	manifest, err := loadAppManifestFromFile(path)
	if err != nil {
		return err
	}

	if err := validateAppName(manifest.Name); err != nil {
		return err
	}

	client, err := newClientFromProfile(profile)
	if err != nil {
		return err
	}

	props := manifest.Properties
	if props == nil {
		props = map[string]any{}
	}

	// renderName 默认与 name 一致
	if _, ok := props["renderName"]; !ok {
		props["renderName"] = manifest.Name
	}

	if apiErr := client.CreateApp(manifest.Name, props); apiErr != nil {
		return apiErr
	}

	fmt.Printf("App '%s' created successfully\n", manifest.Name)
	return nil
}

func runAppCreate(name, description, renderName, profile string) error {
	if err := validateAppName(name); err != nil {
		return err
	}

	client, err := newClientFromProfile(profile)
	if err != nil {
		return err
	}

	// renderName 默认与 name 一致
	if renderName == "" {
		renderName = name
	}

	props := map[string]any{
		"renderName": renderName,
	}
	if description != "" {
		props["description"] = description
	}

	if apiErr := client.CreateApp(name, props); apiErr != nil {
		return apiErr
	}

	fmt.Printf("App '%s' created successfully\n", name)
	return nil
}
