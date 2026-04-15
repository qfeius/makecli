/**
 * [INPUT]: 依赖 github.com/spf13/cobra、fmt、path/filepath
 * [OUTPUT]: 对外提供 newAppCmd 函数、loadAppManifestFromFile helper
 * [POS]: cmd 模块的 app 命令组，挂载 create / list / delete / init 等子命令；提供从 YAML 加载 App 清单的共享逻辑
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/spf13/cobra"
)

// validAppName 仅允许英文字母、数字、下划线，长度 3-20
var validAppName = regexp.MustCompile(`^[A-Za-z0-9_]{3,20}$`)

// validateAppName 校验 App name 格式
func validateAppName(name string) error {
	if !validAppName.MatchString(name) {
		return fmt.Errorf("invalid app name %q: 仅支持英文字母、数字、下划线，长度 3-20", name)
	}
	return nil
}

func newAppCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "app",
		Short: "Manage apps",
	}
	cmd.AddCommand(newAppCreateCmd())
	cmd.AddCommand(newAppListCmd())
	cmd.AddCommand(newAppInitCmd())
	cmd.AddCommand(newAppDeleteCmd())
	return cmd
}

// loadAppManifestFromFile 从 YAML 文件加载唯一的 Make.App 资源清单
func loadAppManifestFromFile(path string) (ResourceManifest, error) {
	ext := filepath.Ext(path)
	if !isRecognizedManifestExtension(ext) {
		return ResourceManifest{}, fmt.Errorf("文件必须为 .yaml 或 .yml 格式")
	}

	manifests, err := loadManifestsFromFile(path)
	if err != nil {
		return ResourceManifest{}, err
	}

	var apps []ResourceManifest
	for _, m := range manifests {
		if m.Type == "Make.App" {
			apps = append(apps, m)
		}
	}

	if len(apps) == 0 {
		return ResourceManifest{}, fmt.Errorf("文件中未找到 Make.App 资源")
	}
	if len(apps) > 1 {
		return ResourceManifest{}, fmt.Errorf("文件中包含多个 Make.App 资源，期望恰好一个")
	}

	return apps[0], nil
}
