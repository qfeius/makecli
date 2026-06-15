/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile/newRepoClientFromProfile）、cmd/app（loadAppManifestFromFile、validResourceKey）、internal/api（CodeRepoResource）、fmt、os、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newAppCreateCmd 函数
 * [POS]: cmd/app 的 create 子命令，调用 Meta Server API 创建 App。位置参数是 App key（英文标识符），--name 为展示名（支持中文）；
 *        key 缺省且 --name 是合法标识符时直接用 name 作 key；支持 --description 和 -f 文件模式；
 *        创建成功后调用代码仓库服务幂等准备 preview/production 双环境仓库（失败降级为警告，deploy 时自动重试）
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"
	"os"

	"github.com/qfeius/makecli/internal/api"
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
  makecli app create --name myapp
  makecli app create myapp --name "我的应用"
  makecli app create myapp --name "My App" --description "my awesome app"
  makecli app create -f app.yaml`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if file != "" {
				return runAppCreateFromFile(file)
			}
			key := displayName // key 缺省时回退用 --name（须为合法标识符，下游校验）
			if len(args) > 0 {
				key = args[0]
			}
			if key == "" {
				return fmt.Errorf("requires app key, --name or -f flag")
			}
			return runAppCreate(key, displayName, description)
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
	displayName := defaultName(manifest.Name, manifest.Key)

	if apiErr := client.CreateApp(manifest.Key, displayName, props); apiErr != nil {
		return apiErr
	}

	fmt.Printf("App '%s' created successfully\n", manifest.Key)
	prepareCodeRepos(manifest.Key)
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
	displayName = defaultName(displayName, key)

	props := map[string]any{}
	if description != "" {
		props["description"] = description
	}

	if apiErr := client.CreateApp(key, displayName, props); apiErr != nil {
		return apiErr
	}

	fmt.Printf("App '%s' created successfully\n", key)
	prepareCodeRepos(key)
	return nil
}

// prepareCodeRepos 在 App 创建成功后幂等准备 preview/production 代码仓库。
// 仓库服务故障不该把已成功的 App 创建报成失败——deploy 走同一个幂等接口会自动重试，
// 这里只降级为 stderr 警告。
func prepareCodeRepos(appKey string) {
	client, _, err := newRepoClientFromProfile()
	if err == nil {
		var repo *api.CodeRepoResource
		if repo, err = client.CreateRepository(appKey); err == nil {
			printCodeRepos(repo)
			return
		}
	}
	fmt.Fprintf(os.Stderr, "warning: code repositories not ready: %v (deploy will retry automatically)\n", err)
}

// printCodeRepos 打印各环境仓库地址，单仓库兼容形态下两个环境显示同一地址
func printCodeRepos(repo *api.CodeRepoResource) {
	lines := ""
	for _, env := range deployEnvs {
		if url := repo.CloneURLFor(env); url != "" {
			lines += fmt.Sprintf("  %-12s %s\n", env+":", url)
		}
	}
	if lines != "" {
		fmt.Print("Code repositories ready:\n" + lines)
	}
}
