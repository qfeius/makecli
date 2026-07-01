/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、cmd/app（validResourceKey）、internal/api（Client/ErrNotFound/CreateApp/CreateEntity/GetApp/GetEntity/UpdateEntity/GetRelation/CreateRelation/UpdateRelation/Field/EntityProperties/UniqueConstraint/RelationProperties/RelationEnd）、errors、fmt、io、io/fs、math、os、path/filepath、slices、strings、gopkg.in/yaml.v3、github.com/spf13/cobra
 * [OUTPUT]: 对外提供 newApplyCmd 函数、ResourceManifest 类型（Key/Name/Type/AppKey/Meta/Properties）、extractUniqueConstraints/loadManifestsFromFile/loadManifestsFromDir/subdirLevel helper
 * [POS]: cmd 模块的顶层 apply 命令，从 YAML 文件/目录批量应用资源（create-or-update 语义），按 Key 标识资源，Name 仅为展示名；存在性判定依赖 api.ErrNotFound，瞬时/传输错误不会被误判为"不存在"；目录扫描经 filepath.WalkDir 递归，--max-depth 控制层级（1=当前目录/2=含子目录默认/0=不限），隐藏文件与隐藏子目录跳过
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
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
	var maxDepth int

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
			return runAppApply(path, maxDepth)
		},
	}

	cmd.Flags().StringVarP(&path, "file", "f", "", "path to YAML file or directory (required)")
	cmd.Flags().IntVar(&maxDepth, "max-depth", 2, "directory recursion depth (1=top level, 2=+subdirs, 0=unlimited)")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

// ---------------------------------- 执行函数 ----------------------------------

func runAppApply(path string, maxDepth int) error {
	if maxDepth < 0 {
		return fmt.Errorf("--max-depth 不能为负数（0=递归全部，1=当前目录，2=含子目录）")
	}

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
		resources, err = loadManifestsFromDir(path, maxDepth)
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

// loadManifestsFromDir 递归扫描目录加载 YAML 文件，maxDepth 控制层级：
// 1=仅当前目录，2=含直接子目录，N=向下 N 层，0=不限深度（递归整棵树）。
// 隐藏文件与隐藏子目录（.git / .idea 等）一律跳过，不下钻。
func loadManifestsFromDir(dir string, maxDepth int) ([]ResourceManifest, error) {
	depthLimit := maxDepth
	if depthLimit == 0 {
		depthLimit = math.MaxInt // 0 = 不限深度，一次性折叠成上界，热路径只剩单条比较
	}

	var manifests []ResourceManifest
	matchedFiles := 0

	walkErr := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("读取目录失败: %w", err)
		}
		if entry.IsDir() {
			if path == dir {
				return nil // 根目录恒为第 1 层，永不因隐藏名或深度被剪
			}
			if strings.HasPrefix(entry.Name(), ".") {
				return fs.SkipDir // 不下钻隐藏目录（.git 等）
			}
			if subdirLevel(dir, path) > depthLimit {
				return fs.SkipDir // 超出深度整棵剪掉
			}
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		if !slices.Contains(recognizedManifestExtensions, filepath.Ext(entry.Name())) {
			return nil
		}
		matchedFiles++
		ms, err := loadManifestsFromFile(path)
		if err != nil {
			return fmt.Errorf("加载 %s 失败: %w", path, err)
		}
		manifests = append(manifests, ms...)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
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

// subdirLevel 返回子目录相对根的层级：直接子目录=2，再深一层=3……
// 根目录（level 1）由调用方短路，不会进入此函数。
func subdirLevel(root, path string) int {
	rel, _ := filepath.Rel(root, path)
	return strings.Count(rel, string(filepath.Separator)) + 2
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
