/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、cmd/app（loadAppManifestFromFile）、fmt、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newAppDeleteCmd 函数
 * [POS]: cmd/app 的 delete 子命令，调用 Meta Server API 按 key 删除指定 App，支持 -f 文件模式
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newAppDeleteCmd() *cobra.Command {
	var file string

	cmd := &cobra.Command{
		Use:   "delete [key]",
		Short: "Delete an app on Make",
		Example: `  makecli app delete myapp
  makecli app delete -f app.yaml`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if file != "" {
				return runAppDeleteFromFile(file)
			}
			if len(args) == 0 {
				return fmt.Errorf("requires app key or -f flag")
			}
			return runAppDelete(args[0])
		},
	}

	cmd.Flags().StringVarP(&file, "file", "f", "", "path to YAML file containing Make.App resource")
	return cmd
}

func runAppDeleteFromFile(path string) error {
	manifest, err := loadAppManifestFromFile(path)
	if err != nil {
		return err
	}
	return runAppDelete(manifest.Key)
}

func runAppDelete(key string) error {
	client, err := newClientFromProfile()
	if err != nil {
		return err
	}

	if err := client.DeleteApp(key); err != nil {
		return err
	}

	fmt.Printf("App '%s' deleted successfully\n", key)
	return nil
}
