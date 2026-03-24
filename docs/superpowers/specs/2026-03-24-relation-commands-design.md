# makecli relation 命令设计

## 概述

为 makecli 新增 `relation` 顶层命令组，提供 Relation 资源的 CRUD 能力（create / update / delete / list+get）。Relation 是与 Entity 平级的 Make Meta Service 顶层资源，描述 Entity 之间的关系。

## 范围

**包含：**
- API 层：Relation 数据类型 + 5 个 CRUD 方法
- CMD 层：4 个子命令（create / update / delete / list）+ 命令组
- 测试：每个子命令对应测试文件
- 注册：root.go 挂载 newRelationCmd()

**不包含：**
- apply.go / diff.go 的 Make.Relation 支持（后续迭代）

## 数据模型

### API 类型（`internal/api/client.go`）

```go
type RelationEnd struct {
    Entity      string `json:"entity"`
    Cardinality string `json:"cardinality"` // "one" | "many"
}

type RelationProperties struct {
    From RelationEnd `json:"from"`
    To   RelationEnd `json:"to"`
}

type Relation struct {
    Name       string             `json:"name"`
    Type       string             `json:"type"`
    App        string             `json:"app"`
    Meta       map[string]any     `json:"meta"`
    Properties RelationProperties `json:"properties"`
}
```

### `--json` 文件格式

输入文件内容为 `RelationProperties`：

```json
{
  "from": { "entity": "项目", "cardinality": "many" },
  "to": { "entity": "任务", "cardinality": "one" }
}
```

## API 方法

| 方法签名 | Target Header | Path | 请求体 |
|---------|--------------|------|--------|
| `CreateRelation(name, app string, props RelationProperties) error` | MakeService.CreateResource | /meta/v1/relation | 完整 Relation JSON |
| `UpdateRelation(name, app string, props RelationProperties) error` | MakeService.UpdateResource | /meta/v1/relation | 完整 Relation JSON |
| `GetRelation(app, name string) (*Relation, error)` | MakeService.GetResource | /meta/v1/relation | `{app, name}` |
| `ListRelations(app string, page, size int) ([]Relation, int, error)` | MakeService.ListResources | /meta/v1/relation | `{app, sort, pagination}` |
| `DeleteRelation(name, app string) error` | MakeService.DeleteResource | /meta/v1/relation | `{name, type, app}` |

## CLI 命令

### 命令组：`relation.go`

```
makecli relation --app <app> <subcommand>
```

- `--app` 为 PersistentFlag，所有子命令继承
- PersistentPreRunE 校验 `--app` 非空

### 子命令

```bash
# 创建
makecli relation create <name> --app <app> --json <file.json> [--profile default]

# 更新
makecli relation update <name> --app <app> --json <file.json> [--profile default]

# 删除
makecli relation delete <name> --app <app> [--profile default]

# 列表
makecli relation list --app <app> [--page 1] [--size 20] [--output table|json] [--profile default]

# 详情
makecli relation list <name> --app <app> [--output table|json] [--profile default]
```

### 输出格式

**list 列表模式（表格）：**

```
NAME                  FROM         TO           VERSION
project-has-tasks     项目(many)   任务(one)    1.0.0
```

**list 详情模式：**

```
Name:         project-has-tasks
App:          myapp
Version:      1.0.0

From:
  Entity:      项目
  Cardinality: many

To:
  Entity:      任务
  Cardinality: one
```

## 文件结构

### 新增文件

```
cmd/
├── relation.go              # 命令组，--app PersistentFlag
├── relation_create.go       # create 子命令
├── relation_create_test.go
├── relation_update.go       # update 子命令
├── relation_update_test.go
├── relation_delete.go       # delete 子命令
├── relation_delete_test.go
├── relation_list.go         # list（无参=列表，有参=详情）
├── relation_list_test.go
```

### 修改文件

```
cmd/root.go                  # 新增 rootCmd.AddCommand(newRelationCmd())
internal/api/client.go       # 新增 Relation 类型 + 5 个方法
```

## 测试策略

与 entity 测试完全同构：

- **httptest.Server** 模拟 API，校验 target header / path / body
- **t.Setenv("HOME", t.TempDir())** 隔离 credentials
- **captureStdout()** 捕获输出，断言表格/JSON 内容
- 每个子命令覆盖正常路径 + 错误路径（缺 `--app`、缺 `--json`、API 错误）

## 约束

- **`--json` 是 create/update 的必需参数**：与 entity 的 `--fields`（可选）不同，relation 没有 from/to 无意义。使用 `cmd.MarkFlagRequired("json")` 强制校验。
- **ListRelations 排序字段**：使用 `sort: [{"field": "id", "order": "asc"}]`，与 entity/app 的 ListEntities/ListApps 保持一致（API 文档示例中的 `createdAt` 仅为示意）。
- **VERSION 列取自 `meta.version`**：表格和详情视图中的版本号均从 `Relation.Meta["version"]` 提取，与 entity list 行为一致。

## 设计决策

1. **`--json` 文件输入（非命令行参数）**：与 entity 的 `--fields` 模式一致，通过 apply 命令覆盖批量场景
2. **list 复用详情（位置参数）**：与 entity list 行为一致，不引入独立 get 子命令
3. **包含 update 子命令**：entity 没有但 relation 需要，因为 relation 的 from/to 属性修改是常见操作
4. **不做泛型抽象**：仅两种资源，镜像实现成本低于抽象成本
5. **不做 apply/diff 支持**：独立增量，后续迭代
