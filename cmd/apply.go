/**
 * [INPUT]: 依赖 internal/config（Load）、internal/api（Client/CreateAppWithCode/CreateEntity/GetApp/GetEntity/UpdateEntity）、fmt、os、path/filepath、strings、gopkg.in/yaml.v3、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newApplyCmd 函数
 * [POS]: cmd 模块的顶层 apply 命令，从 YAML 文件/目录批量应用资源（create-or-update 语义）
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/MakeHQ/makecli/internal/api"
	"github.com/MakeHQ/makecli/internal/config"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// ---------------------------------- 命令定义 ----------------------------------

func newApplyCmd() *cobra.Command {
	var profile string
	var server string
	var path string

	cmd := &cobra.Command{
		Use:   "apply -f <path>",
		Short: "Apply resources from YAML file or directory",
		Long: `Apply resources defined in YAML files or directories.
Supports creating App and Entity resources.`,
		Example: `  makecli apply -f app.yaml
  makecli apply -f ./configs/
  makecli apply --dry-run -f app.yaml`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppApply(path, profile, server)
		},
	}

	cmd.Flags().StringVar(&profile, "profile", "default", "credentials profile to use")
	cmd.Flags().StringVar(&server, "server", defaultMetaServer, "Meta Server base URL")
	cmd.Flags().StringVarP(&path, "file", "f", "", "path to YAML file or directory (required)")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

// ---------------------------------- 执行函数 ----------------------------------

func runAppApply(path, profile, server string) error {
	creds, err := config.Load()
	if err != nil {
		return fmt.Errorf("加载凭证失败: %w", err)
	}

	p, ok := creds[profile]
	if !ok || p.AccessToken == "" {
		return fmt.Errorf("profile '%s' 未配置，请先运行: makecli configure --profile %s", profile, profile)
	}

	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("路径不存在: %w", err)
	}

	var resources []ResourceManifest
	if info.IsDir() {
		resources, err = loadManifestsFromDir(path)
	} else {
		resources, err = loadManifestsFromFile(path)
	}
	if err != nil {
		return err
	}

	if len(resources) == 0 {
		return fmt.Errorf("error: no objects passed to apply")
	}

	if err := applyResources(resources, server, p.AccessToken); err != nil {
		return err
	}

	fmt.Printf("Applied %d resources successfully\n", len(resources))
	return nil
}

// ---------------------------------- 资源清单 ----------------------------------

// ResourceManifest YAML 资源清单的通用结构
type ResourceManifest struct {
	Name       string         `yaml:"name"`
	Type       string         `yaml:"type"`
	App        string         `yaml:"app,omitempty"`
	Meta       map[string]any `yaml:"meta"`
	Properties map[string]any `yaml:"properties"`
}

var recognizedManifestExtensions = []string{".yaml", ".yml"}

// ---------------------------------- YAML 解析 ----------------------------------

// loadManifestsFromFile 从文件加载多文档 YAML
func loadManifestsFromFile(path string) ([]ResourceManifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}

	var manifests []ResourceManifest
	decoder := yaml.NewDecoder(strings.NewReader(string(data)))
	for {
		var m ResourceManifest
		if err := decoder.Decode(&m); err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, fmt.Errorf("解析 YAML 失败: %w", err)
		}
		// 跳过空文档
		if m.Name == "" || m.Type == "" {
			continue
		}
		manifests = append(manifests, m)
	}
	return manifests, nil
}

// loadManifestsFromDir 从目录扫描一层加载所有 YAML 文件
func loadManifestsFromDir(dir string) ([]ResourceManifest, error) {
	var manifests []ResourceManifest
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("读取目录失败: %w", err)
	}

	matchedFiles := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		ext := filepath.Ext(entry.Name())
		if !isRecognizedManifestExtension(ext) {
			continue
		}
		matchedFiles++
		ms, err := loadManifestsFromFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("加载 %s 失败: %w", entry.Name(), err)
		}
		manifests = append(manifests, ms...)
	}
	if matchedFiles == 0 {
		return nil, fmt.Errorf(
			"error reading [%s]: recognized file extensions are [%s]",
			dir,
			strings.Join(recognizedManifestExtensions, " "),
		)
	}
	return manifests, nil
}

func isRecognizedManifestExtension(ext string) bool {
	for _, candidate := range recognizedManifestExtensions {
		if ext == candidate {
			return true
		}
	}
	return false
}

// ---------------------------------- 资源应用 ----------------------------------

// applyResources 按依赖顺序应用资源：Make.App 先于 Make.Entity
func applyResources(resources []ResourceManifest, server, token string) error {
	// 按类型分组
	apps := []ResourceManifest{}
	entities := []ResourceManifest{}
	for _, r := range resources {
		if r.Type == "Make.App" {
			apps = append(apps, r)
		} else if r.Type == "Make.Entity" {
			entities = append(entities, r)
		}
	}

	client := api.New(server, token, DebugMode)

	// 先应用 App
	for _, app := range apps {
		action, err := applyApp(app, client)
		if err != nil {
			return fmt.Errorf("应用 App '%s' 失败: %w", app.Name, err)
		}
		if action != "" {
			fmt.Printf("App '%s' %s\n", app.Name, action)
		}
	}

	// 再应用 Entity
	for _, entity := range entities {
		action, err := applyEntity(entity, client)
		if err != nil {
			return fmt.Errorf("应用 Entity '%s' 失败: %w", entity.Name, err)
		}
		fmt.Printf("Entity '%s' %s\n", entity.Name, action)
	}

	return nil
}

// applyApp 从清单应用 App：不存在则创建，已存在则跳过（App 无 update API）
func applyApp(manifest ResourceManifest, client *api.Client) (string, error) {
	code, _ := manifest.Properties["code"].(string)
	if code == "" {
		code = manifest.Name
	}

	existing, err := client.GetApp(manifest.Name)
	if err == nil && existing.Name != "" {
		return "", nil // App 无 update API，静默跳过
	}

	return "created", client.CreateAppWithCode(manifest.Name, code)
}

// applyEntity 从清单应用 Entity：不存在则创建，已存在则更新
func applyEntity(manifest ResourceManifest, client *api.Client) (string, error) {
	if manifest.App == "" {
		return "", fmt.Errorf("Entity 缺少 app 字段")
	}

	fieldsRaw, ok := manifest.Properties["fields"]
	if !ok {
		return "", fmt.Errorf("Entity 缺少 fields 字段")
	}

	fieldsSlice, ok := fieldsRaw.([]any)
	if !ok {
		return "", fmt.Errorf("fields 必须为数组")
	}

	fields := make([]api.Field, len(fieldsSlice))
	for i, f := range fieldsSlice {
		fieldMap, ok := f.(map[string]any)
		if !ok {
			return "", fmt.Errorf("field[%d] 必须为对象", i)
		}
		fields[i] = api.Field{
			Name:       getField(fieldMap, "name").(string),
			Type:       getField(fieldMap, "type").(string),
			Meta:       getFieldMap(fieldMap, "meta"),
			Properties: getFieldMap(fieldMap, "properties"),
		}
	}

	existing, err := client.GetEntity(manifest.App, manifest.Name)
	if err == nil && existing.Name != "" {
		return "updated", client.UpdateEntity(manifest.Name, manifest.App, fields)
	}

	return "created", client.CreateEntity(manifest.Name, manifest.App, fields)
}

// getField 安全获取字段值
func getField(m map[string]any, key string) any {
	v, ok := m[key]
	if !ok {
		return nil
	}
	return v
}

// getFieldMap 安全获取 map[string]any 类型字段
func getFieldMap(m map[string]any, key string) map[string]any {
	v := getField(m, key)
	if v == nil {
		return nil
	}
	m2, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return m2
}
