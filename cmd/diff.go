/**
 * [INPUT]: 依赖 cmd/client（newClientFromProfile）、internal/api（Client/GetApp/ListEntities/ListRelations/Entity/Relation/RelationProperties/RelationEnd）、cmd/apply（loadManifestsFromFile/Dir/ResourceManifest/getFieldMap）、cmd/output（validateOutputFormat/writeJSON）、encoding/json、fmt、os、reflect、sort、strings
 * [OUTPUT]: 对外提供 newDiffCmd 函数
 * [POS]: cmd 模块的顶层 diff 命令，对比远端 Meta Server 上的 App DSL（Entity + Relation）与本地 YAML 文件的差异
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/qfeius/makecli/internal/api"
	"github.com/spf13/cobra"
)

// ---------------------------------- 命令定义 ----------------------------------

func newDiffCmd() *cobra.Command {
	var profile string
	var path string
	var output string

	cmd := &cobra.Command{
		Use:   "diff -f <path>",
		Short: "Compare local DSL files with remote App definition",
		Long: `Compare local YAML resource definitions with the remote App on Meta Server.
The app name is inferred from the Make.App manifest or entity's app field in the YAML files.`,
		Example: `  makecli diff -f ./dsl/
  makecli diff -f app.yaml --output json`,
		Args:         cobra.NoArgs,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(path, profile, output)
		},
	}

	cmd.Flags().StringVarP(&path, "file", "f", "", "path to YAML file or directory (required)")
	cmd.Flags().StringVar(&profile, "profile", "default", "credentials profile to use")
	cmd.Flags().StringVar(&output, "output", outputTable, "output format (table|json)")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

// ---------------------------------- Diff 数据类型 ----------------------------------

// DiffResult 整体对比结果
type DiffResult struct {
	AppName   string         `json:"app"`
	Entities  []EntityDiff   `json:"entities"`
	Relations []RelationDiff `json:"relations"`
	Summary   DiffSummary    `json:"summary"`
}

// EntityDiff 单个 Entity 的对比结果
type EntityDiff struct {
	Name   string      `json:"name"`
	Status string      `json:"status"` // added | removed | changed | unchanged
	Fields []FieldDiff `json:"fields,omitempty"`
}

// FieldDiff 单个 Field 的对比结果
type FieldDiff struct {
	Name   string `json:"name"`
	Status string `json:"status"` // added | removed | changed
	Detail string `json:"detail,omitempty"`
}

// RelationDiff 单个 Relation 的对比结果
type RelationDiff struct {
	Name   string `json:"name"`
	Status string `json:"status"` // added | removed | changed | unchanged
	Detail string `json:"detail,omitempty"`
}

// DiffSummary 差异统计
type DiffSummary struct {
	Added     int `json:"added"`
	Removed   int `json:"removed"`
	Changed   int `json:"changed"`
	Unchanged int `json:"unchanged"`
}

// ---------------------------------- 状态常量 ----------------------------------

const (
	diffAdded     = "added"     // 仅本地有
	diffRemoved   = "removed"   // 仅远端有
	diffChanged   = "changed"   // 两端都有但不同
	diffUnchanged = "unchanged" // 完全一致
)

// ---------------------------------- 执行函数 ----------------------------------

func runDiff(path, profile, output string) error {
	if err := validateOutputFormat(output); err != nil {
		return err
	}

	// 构建客户端
	client, err := newClientFromProfile(profile)
	if err != nil {
		return err
	}

	// 加载本地资源
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

	// 从 YAML 推断 app name
	app := resolveAppName(resources)
	if app == "" {
		return fmt.Errorf("无法推断 App 名称：YAML 中未找到 Make.App 定义或 entity 的 app 字段")
	}

	// 提取本地 Entity / Relation 清单
	localEntities := filterEntities(resources)
	localRelations := filterRelations(resources)

	// 获取远端数据
	if _, err := client.GetApp(app); err != nil {
		return fmt.Errorf("获取远端 App '%s' 失败: %w", app, err)
	}
	remoteEntities, err := fetchAllEntities(client, app)
	if err != nil {
		return fmt.Errorf("获取远端 Entity 失败: %w", err)
	}
	remoteRelations, err := fetchAllRelations(client, app)
	if err != nil {
		return fmt.Errorf("获取远端 Relation 失败: %w", err)
	}

	// 计算差异
	entityResult := computeDiff(app, localEntities, remoteEntities)
	relationDiffs, relationSummary := computeRelationDiff(localRelations, remoteRelations)
	result := DiffResult{
		AppName:   app,
		Entities:  entityResult.Entities,
		Relations: relationDiffs,
		Summary: DiffSummary{
			Added:     entityResult.Summary.Added + relationSummary.Added,
			Removed:   entityResult.Summary.Removed + relationSummary.Removed,
			Changed:   entityResult.Summary.Changed + relationSummary.Changed,
			Unchanged: entityResult.Summary.Unchanged + relationSummary.Unchanged,
		},
	}

	// 输出
	if output == outputJSON {
		return writeJSON(result)
	}
	renderDiffTable(&result)

	// 有差异时退出码 1
	if result.Summary.Added > 0 || result.Summary.Removed > 0 || result.Summary.Changed > 0 {
		os.Exit(1)
	}
	return nil
}

// ---------------------------------- 远端数据获取 ----------------------------------

// fetchAllEntities 分页获取指定 App 下的全部 Entity
func fetchAllEntities(client *api.Client, app string) ([]api.Entity, error) {
	var all []api.Entity
	page := 1
	for {
		batch, total, err := client.ListEntities(app, page, 100, nil)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(all) >= total {
			break
		}
		page++
	}
	return all, nil
}

// ---------------------------------- 本地数据过滤 ----------------------------------

// filterEntities 从混合资源清单中提取 Entity 类型的 Manifest
func filterEntities(resources []ResourceManifest) []ResourceManifest {
	var entities []ResourceManifest
	for _, r := range resources {
		if r.Type == "Make.Entity" {
			entities = append(entities, r)
		}
	}
	return entities
}

// filterRelations 从混合资源清单中提取 Relation 类型的 Manifest
func filterRelations(resources []ResourceManifest) []ResourceManifest {
	var relations []ResourceManifest
	for _, r := range resources {
		if r.Type == "Make.Relation" {
			relations = append(relations, r)
		}
	}
	return relations
}

// fetchAllRelations 分页获取指定 App 下的全部 Relation
func fetchAllRelations(client *api.Client, app string) ([]api.Relation, error) {
	var all []api.Relation
	page := 1
	for {
		batch, total, err := client.ListRelations(app, page, 100, nil)
		if err != nil {
			return nil, err
		}
		all = append(all, batch...)
		if len(all) >= total {
			break
		}
		page++
	}
	return all, nil
}

// resolveAppName 从资源清单推断 App 名称
// 优先级: Make.App 的 name > 第一个 Make.Entity 的 app 字段 > 第一个 Make.Relation 的 app 字段
func resolveAppName(resources []ResourceManifest) string {
	for _, r := range resources {
		if r.Type == "Make.App" && r.Name != "" {
			return r.Name
		}
	}
	for _, r := range resources {
		if (r.Type == "Make.Entity" || r.Type == "Make.Relation") && r.App != "" {
			return r.App
		}
	}
	return ""
}

// ---------------------------------- 核心对比 ----------------------------------

// computeDiff 对比本地和远端的 Entity 集合，产出 DiffResult
func computeDiff(app string, local []ResourceManifest, remote []api.Entity) DiffResult {
	// 建索引
	remoteByName := make(map[string]api.Entity, len(remote))
	for _, e := range remote {
		remoteByName[e.Name] = e
	}
	localByName := make(map[string]ResourceManifest, len(local))
	for _, m := range local {
		localByName[m.Name] = m
	}

	var diffs []EntityDiff
	visited := make(map[string]bool)

	// 遍历本地: 找 added 和 changed
	for _, m := range local {
		visited[m.Name] = true
		re, exists := remoteByName[m.Name]
		if !exists {
			diffs = append(diffs, EntityDiff{Name: m.Name, Status: diffAdded})
			continue
		}
		fieldDiffs := compareFields(m, &re)
		status := diffUnchanged
		if len(fieldDiffs) > 0 {
			status = diffChanged
		}
		diffs = append(diffs, EntityDiff{Name: m.Name, Status: status, Fields: fieldDiffs})
	}

	// 遍历远端: 找 removed
	for _, e := range remote {
		if visited[e.Name] {
			continue
		}
		diffs = append(diffs, EntityDiff{Name: e.Name, Status: diffRemoved})
	}

	// 排序: changed > added > removed > unchanged
	sort.Slice(diffs, func(i, j int) bool {
		return diffOrder(diffs[i].Status) < diffOrder(diffs[j].Status)
	})

	// 统计
	var summary DiffSummary
	for _, d := range diffs {
		switch d.Status {
		case diffAdded:
			summary.Added++
		case diffRemoved:
			summary.Removed++
		case diffChanged:
			summary.Changed++
		case diffUnchanged:
			summary.Unchanged++
		}
	}

	return DiffResult{AppName: app, Entities: diffs, Summary: summary}
}

// computeRelationDiff 对比本地和远端的 Relation 集合
func computeRelationDiff(local []ResourceManifest, remote []api.Relation) ([]RelationDiff, DiffSummary) {
	remoteByName := make(map[string]api.Relation, len(remote))
	for _, r := range remote {
		remoteByName[r.Name] = r
	}

	var diffs []RelationDiff
	visited := make(map[string]bool)

	for _, m := range local {
		visited[m.Name] = true
		rr, exists := remoteByName[m.Name]
		if !exists {
			diffs = append(diffs, RelationDiff{Name: m.Name, Status: diffAdded})
			continue
		}
		detail := compareRelationEndpoints(m, &rr)
		status := diffUnchanged
		if detail != "" {
			status = diffChanged
		}
		diffs = append(diffs, RelationDiff{Name: m.Name, Status: status, Detail: detail})
	}

	for _, r := range remote {
		if visited[r.Name] {
			continue
		}
		diffs = append(diffs, RelationDiff{Name: r.Name, Status: diffRemoved})
	}

	sort.Slice(diffs, func(i, j int) bool {
		return diffOrder(diffs[i].Status) < diffOrder(diffs[j].Status)
	})

	var summary DiffSummary
	for _, d := range diffs {
		switch d.Status {
		case diffAdded:
			summary.Added++
		case diffRemoved:
			summary.Removed++
		case diffChanged:
			summary.Changed++
		case diffUnchanged:
			summary.Unchanged++
		}
	}

	return diffs, summary
}

// compareRelationEndpoints 对比 Relation 的 from/to 端点，返回变化描述
func compareRelationEndpoints(local ResourceManifest, remote *api.Relation) string {
	localFrom := getFieldMap(local.Properties, "from")
	localTo := getFieldMap(local.Properties, "to")

	var changes []string

	localFromEntity, _ := localFrom["entity"].(string)
	localFromCard, _ := localFrom["cardinality"].(string)
	if localFromEntity != remote.Properties.From.Entity || localFromCard != remote.Properties.From.Cardinality {
		changes = append(changes, fmt.Sprintf("from: %s(%s) → %s(%s)",
			remote.Properties.From.Entity, remote.Properties.From.Cardinality,
			localFromEntity, localFromCard))
	}

	localToEntity, _ := localTo["entity"].(string)
	localToCard, _ := localTo["cardinality"].(string)
	if localToEntity != remote.Properties.To.Entity || localToCard != remote.Properties.To.Cardinality {
		changes = append(changes, fmt.Sprintf("to: %s(%s) → %s(%s)",
			remote.Properties.To.Entity, remote.Properties.To.Cardinality,
			localToEntity, localToCard))
	}

	return strings.Join(changes, "; ")
}

// diffOrder 差异状态排序权重
func diffOrder(status string) int {
	switch status {
	case diffChanged:
		return 0
	case diffAdded:
		return 1
	case diffRemoved:
		return 2
	case diffUnchanged:
		return 3
	default:
		return 4
	}
}

// compareFields 对比本地 Manifest 和远端 Entity 的字段列表
func compareFields(local ResourceManifest, remote *api.Entity) []FieldDiff {
	// 解析本地 fields
	localFields := extractLocalFields(local)

	// 构建远端索引
	remoteByName := make(map[string]api.Field, len(remote.Properties.Fields))
	for _, f := range remote.Properties.Fields {
		remoteByName[f.Name] = f
	}

	var diffs []FieldDiff
	visited := make(map[string]bool)

	// 本地 → 远端
	for _, lf := range localFields {
		visited[lf.Name] = true
		rf, exists := remoteByName[lf.Name]
		if !exists {
			diffs = append(diffs, FieldDiff{
				Name:   lf.Name,
				Status: diffAdded,
				Detail: lf.Type,
			})
			continue
		}
		if detail := fieldChanges(lf, rf); detail != "" {
			diffs = append(diffs, FieldDiff{
				Name:   lf.Name,
				Status: diffChanged,
				Detail: detail,
			})
		}
	}

	// 远端 → 本地
	for _, rf := range remote.Properties.Fields {
		if visited[rf.Name] {
			continue
		}
		diffs = append(diffs, FieldDiff{
			Name:   rf.Name,
			Status: diffRemoved,
			Detail: rf.Type,
		})
	}

	return diffs
}

// localField 从 YAML manifest 提取的字段定义
type localField struct {
	Name        string
	Type        string
	Meta        map[string]any
	Properties  map[string]any
	Validations map[string]any
}

// extractLocalFields 从 ResourceManifest 的 properties.fields 解析出字段列表
func extractLocalFields(m ResourceManifest) []localField {
	fieldsRaw, ok := m.Properties["fields"]
	if !ok {
		return nil
	}
	fieldsSlice, ok := fieldsRaw.([]any)
	if !ok {
		return nil
	}

	fields := make([]localField, 0, len(fieldsSlice))
	for _, f := range fieldsSlice {
		fm, ok := f.(map[string]any)
		if !ok {
			continue
		}
		name, _ := fm["name"].(string)
		typ, _ := fm["type"].(string)
		fields = append(fields, localField{
			Name:        name,
			Type:        typ,
			Meta:        getFieldMap(fm, "meta"),
			Properties:  getFieldMap(fm, "properties"),
			Validations: getFieldMap(fm, "validations"),
		})
	}
	return fields
}

// fieldChanges 对比两端同名字段，返回变化描述（空串表示无变化）
func fieldChanges(local localField, remote api.Field) string {
	if local.Type != remote.Type {
		return fmt.Sprintf("type: %s → %s", remote.Type, local.Type)
	}
	// Properties 深度对比（JSON 归一化解决 int/float64 差异）
	if !jsonDeepEqual(local.Properties, remote.Properties) {
		return "properties changed"
	}
	if !jsonDeepEqual(local.Validations, remote.Validations) {
		return "validations changed"
	}
	return ""
}

// jsonDeepEqual 通过 JSON 序列化归一化后对比，解决 YAML int vs JSON float64 的类型差异
func jsonDeepEqual(a, b any) bool {
	na := normalize(a)
	nb := normalize(b)
	return reflect.DeepEqual(na, nb)
}

// normalize 通过 JSON 往返消除类型差异
func normalize(v any) any {
	if v == nil {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		return v
	}
	return out
}

// ---------------------------------- 表格渲染 ----------------------------------

// renderDiffTable 以人类可读的格式输出差异
func renderDiffTable(result *DiffResult) {
	fmt.Printf("App: %s\n\n", result.AppName)

	hasDiff := false

	// Entity 差异
	if len(result.Entities) > 0 {
		fmt.Println("Entities:")
		for _, e := range result.Entities {
			switch e.Status {
			case diffChanged:
				hasDiff = true
				fmt.Printf("  ~ %s\n", e.Name)
				for _, f := range e.Fields {
					switch f.Status {
					case diffAdded:
						fmt.Printf("    + %s: %s (only in local)\n", f.Name, f.Detail)
					case diffRemoved:
						fmt.Printf("    - %s: %s (only on server)\n", f.Name, f.Detail)
					case diffChanged:
						fmt.Printf("    ~ %s: %s\n", f.Name, f.Detail)
					}
				}
			case diffAdded:
				hasDiff = true
				fmt.Printf("  + %s (only in local)\n", e.Name)
			case diffRemoved:
				hasDiff = true
				fmt.Printf("  - %s (only on server)\n", e.Name)
			}
		}
	}

	// Relation 差异
	if len(result.Relations) > 0 {
		fmt.Println("\nRelations:")
		for _, r := range result.Relations {
			switch r.Status {
			case diffChanged:
				hasDiff = true
				fmt.Printf("  ~ %s\n", r.Name)
				if r.Detail != "" {
					fmt.Printf("    %s\n", r.Detail)
				}
			case diffAdded:
				hasDiff = true
				fmt.Printf("  + %s (only in local)\n", r.Name)
			case diffRemoved:
				hasDiff = true
				fmt.Printf("  - %s (only on server)\n", r.Name)
			}
		}
	}

	if !hasDiff {
		fmt.Println("  No changes detected.")
	}

	// 汇总
	s := result.Summary
	fmt.Printf("\nSummary: %d changed, %d added, %d removed, %d unchanged\n",
		s.Changed, s.Added, s.Removed, s.Unchanged)
}
