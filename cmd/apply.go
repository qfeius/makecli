/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、cmd/app（validResourceKey）、internal/api（Client/ErrNotFound/CreateApp/CreateEntity/GetApp/GetEntity/UpdateEntity/GetRelation/CreateRelation/UpdateRelation/Field/EntityProperties/UniqueConstraint/RelationProperties/RelationEnd）、errors、fmt、os、path/filepath、strings、gopkg.in/yaml.v3、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newApplyCmd 函数、ResourceManifest 类型（Key/Name/Type/AppKey/Meta/Properties）、extractUniqueConstraints helper
 * [POS]: cmd 模块的顶层 apply 命令，从 YAML 文件/目录批量应用资源（create-or-update 语义），按 Key 标识资源，Name 仅为展示名；存在性判定依赖 api.ErrNotFound，瞬时/传输错误不会被误判为"不存在"
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/qfeius/makecli/internal/api"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// ---------------------------------- 命令定义 ----------------------------------

func newApplyCmd() *cobra.Command {
	var path string

	cmd := &cobra.Command{
		Use:   "apply -f <path>",
		Short: "Apply resources from YAML file or directory",
		Long: `Apply resources defined in YAML files or directories.
Supports creating App, Entity, and Relation resources.`,
		Example: `  makecli apply -f app.yaml
  makecli apply -f ./configs/`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAppApply(path)
		},
	}

	cmd.Flags().StringVarP(&path, "file", "f", "", "path to YAML file or directory (required)")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

// ---------------------------------- 执行函数 ----------------------------------

func runAppApply(path string) error {
	client, err := newClientFromProfile()
	if err != nil {
		return err
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
		return fmt.Errorf("no objects passed to apply")
	}

	if err := applyResources(resources, client); err != nil {
		return err
	}

	fmt.Printf("Applied %d resources successfully\n", len(resources))
	return nil
}

// ---------------------------------- 资源清单 ----------------------------------

// ResourceManifest YAML 资源清单的通用结构
// Key 是英文标识符（创建后不可改），Name 是用户可见展示名（必填，支持中文）
// AppKey 用于 Entity/Relation 引用所属 App 的 key
type ResourceManifest struct {
	Key        string         `yaml:"key"`
	Name       string         `yaml:"name"`
	Type       string         `yaml:"type"`
	AppKey     string         `yaml:"appKey,omitempty"`
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
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("解析 YAML 失败: %w", err)
		}
		// 跳过空文档
		if m.Key == "" || m.Type == "" {
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
		if !slices.Contains(recognizedManifestExtensions, ext) {
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

// ---------------------------------- 资源应用 ----------------------------------

// applyResources 按依赖顺序应用资源：Make.App → Make.Entity → Make.Relation
func applyResources(resources []ResourceManifest, client *api.Client) error {
	// 按类型分组
	apps := []ResourceManifest{}
	entities := []ResourceManifest{}
	relations := []ResourceManifest{}
	for _, r := range resources {
		switch r.Type {
		case "Make.App":
			apps = append(apps, r)
		case "Make.Entity":
			entities = append(entities, r)
		case "Make.Relation":
			relations = append(relations, r)
		default:
			return fmt.Errorf("未知资源类型 '%s'（资源 '%s'），支持的类型: Make.App, Make.Entity, Make.Relation", r.Type, r.Key)
		}
	}

	// 先应用 App
	for _, app := range apps {
		action, err := applyApp(app, client)
		if err != nil {
			return fmt.Errorf("应用 App '%s' 失败: %w", app.Key, err)
		}
		if action != "" {
			fmt.Printf("App '%s' %s\n", app.Key, action)
		}
	}

	// 再应用 Entity
	for _, entity := range entities {
		action, err := applyEntity(entity, client)
		if err != nil {
			return fmt.Errorf("应用 Entity '%s' 失败: %w", entity.Key, err)
		}
		fmt.Printf("Entity '%s' %s\n", entity.Key, action)
	}

	// 最后应用 Relation
	for _, relation := range relations {
		action, err := applyRelation(relation, client)
		if err != nil {
			return fmt.Errorf("应用 Relation '%s' 失败: %w", relation.Key, err)
		}
		fmt.Printf("Relation '%s' %s\n", relation.Key, action)
	}

	return nil
}

// applyApp 从清单应用 App：不存在则创建，已存在则跳过（App 无 update API）
// manifest.Key 是英文标识符，manifest.Name 是展示名；name 缺省时回退用 key
// Get 出现非 not-found 错误时直接上抛，绝不把瞬时故障误判为"不存在"而误建
func applyApp(manifest ResourceManifest, client *api.Client) (string, error) {
	if err := validResourceKey(manifest.Key); err != nil {
		return "", err
	}

	_, err := client.GetApp(manifest.Key)
	if err == nil {
		return "", nil // 已存在；App 无 update API，静默跳过
	}
	if !errors.Is(err, api.ErrNotFound) {
		return "", err // 瞬时/传输/非 not-found 业务错误，上抛不创建
	}

	displayName := defaultName(manifest.Name, manifest.Key)
	return "created", client.CreateApp(manifest.Key, displayName, manifest.Properties)
}

// applyEntity 从清单应用 Entity：不存在则创建，已存在则更新
func applyEntity(manifest ResourceManifest, client *api.Client) (string, error) {
	if manifest.AppKey == "" {
		return "", fmt.Errorf("entity 缺少 appKey 字段")
	}

	fieldsRaw, ok := manifest.Properties["fields"]
	if !ok {
		return "", fmt.Errorf("entity 缺少 fields 字段")
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
		fieldKey, _ := fieldMap["key"].(string)
		fieldName, _ := fieldMap["name"].(string)
		fieldType, _ := fieldMap["type"].(string)
		fields[i] = api.Field{
			Key:         fieldKey,
			Name:        fieldName,
			Type:        fieldType,
			Meta:        getFieldMap(fieldMap, "meta"),
			Properties:  getFieldMap(fieldMap, "properties"),
			Validations: getFieldMap(fieldMap, "validations"),
		}
	}

	constraints, err := extractUniqueConstraints(manifest.Properties)
	if err != nil {
		return "", err
	}

	props := api.EntityProperties{Fields: fields, UniqueConstraints: constraints}
	displayName := defaultName(manifest.Name, manifest.Key)

	_, err = client.GetEntity(manifest.AppKey, manifest.Key)
	if err == nil {
		return "updated", client.UpdateEntity(manifest.Key, displayName, manifest.AppKey, props)
	}
	if !errors.Is(err, api.ErrNotFound) {
		return "", err // 瞬时/传输/非 not-found 业务错误，上抛不创建（避免把 update 降级成 create）
	}

	return "created", client.CreateEntity(manifest.Key, displayName, manifest.AppKey, props)
}

// extractUniqueConstraints 从 YAML properties.uniqueConstraints 解析唯一性约束列表。
// 纯结构性透传（不校验字段存在/类型白名单），合法性由服务端裁决，与 fields 的处理一致。
func extractUniqueConstraints(properties map[string]any) ([]api.UniqueConstraint, error) {
	raw, ok := properties["uniqueConstraints"]
	if !ok || raw == nil {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("uniqueConstraints 必须为数组")
	}

	constraints := make([]api.UniqueConstraint, 0, len(list))
	for i, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("uniqueConstraints[%d] 必须为对象", i)
		}
		name, _ := m["name"].(string)
		fieldsRaw, ok := m["fields"].([]any)
		if !ok {
			return nil, fmt.Errorf("uniqueConstraints[%d].fields 必须为数组", i)
		}
		fields := make([]string, 0, len(fieldsRaw))
		for _, f := range fieldsRaw {
			if fk, ok := f.(string); ok {
				fields = append(fields, fk)
			}
		}
		constraints = append(constraints, api.UniqueConstraint{Name: name, Fields: fields})
	}
	return constraints, nil
}

// applyRelation 从清单应用 Relation：不存在则创建，已存在则更新
func applyRelation(manifest ResourceManifest, client *api.Client) (string, error) {
	if manifest.AppKey == "" {
		return "", fmt.Errorf("relation 缺少 appKey 字段")
	}

	props, err := parseRelationProperties(manifest.Properties)
	if err != nil {
		return "", err
	}

	displayName := defaultName(manifest.Name, manifest.Key)

	_, err = client.GetRelation(manifest.AppKey, manifest.Key)
	if err == nil {
		return "updated", client.UpdateRelation(manifest.Key, displayName, manifest.AppKey, props)
	}
	if !errors.Is(err, api.ErrNotFound) {
		return "", err // 瞬时/传输/非 not-found 业务错误，上抛不创建（避免把 update 降级成 create）
	}

	return "created", client.CreateRelation(manifest.Key, displayName, manifest.AppKey, props)
}

// parseRelationProperties 从 YAML properties map 解析 Relation 的 from/to 端点
// 端点引用通过 entityKey 指向所属 Entity 的 key
func parseRelationProperties(properties map[string]any) (api.RelationProperties, error) {
	fromRaw := getFieldMap(properties, "from")
	if fromRaw == nil {
		return api.RelationProperties{}, fmt.Errorf("relation 缺少 from 字段")
	}
	toRaw := getFieldMap(properties, "to")
	if toRaw == nil {
		return api.RelationProperties{}, fmt.Errorf("relation 缺少 to 字段")
	}

	fromEntityKey, _ := fromRaw["entityKey"].(string)
	fromCardinality, _ := fromRaw["cardinality"].(string)
	toEntityKey, _ := toRaw["entityKey"].(string)
	toCardinality, _ := toRaw["cardinality"].(string)

	return api.RelationProperties{
		From: api.RelationEnd{EntityKey: fromEntityKey, Cardinality: fromCardinality},
		To:   api.RelationEnd{EntityKey: toEntityKey, Cardinality: toCardinality},
	}, nil
}

// getFieldMap 安全获取 map[string]any 类型字段（缺失或类型不符均返回 nil）
func getFieldMap(m map[string]any, key string) map[string]any {
	m2, ok := m[key].(map[string]any)
	if !ok {
		return nil
	}
	return m2
}

// defaultName 返回展示名：name 非空时用 name，否则回退用 key（标识符）。
// 收口「displayName 缺省回退 key」这个在多个 create/apply 路径重复了 8 次的惯用法。
func defaultName(name, key string) string {
	if name == "" {
		return key
	}
	return name
}
