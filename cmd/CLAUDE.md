# cmd/
> L2 | 父级: /CLAUDE.md

## 成员清单
root.go:             根命令入口，挂载所有子命令，对外暴露 Execute(version, date)；定义全局 --debug 标志（隐藏，用于调试，输出 curl 命令）
version.go:          version 子命令，格式化版本输出（参考 GitHub CLI 模式）
version_test.go:     覆盖 formatVersion / changelogURL 的纯函数测试
configure.go:        configure 命令组（PersistentFlag: --profile），默认行为等同 token 子命令；子命令: token（交互写 ~/.make/credentials）/ config（交互写 ~/.make/config）/ set（直接写单个 key）/ get（读取单个 key）；validateConfigKey 校验合法 key 集合
configure_test.go:   覆盖 mask / validateJWT / validateConfigKey 的纯函数测试
client.go:           公共 helper，newClientFromProfile 统一「凭证 + 配置 → API 客户端」构建逻辑，注入 debug/headers 选项
app.go:              app 命令组，挂载 app 相关子命令
app_create.go:       app create 子命令，通过 newClientFromProfile 构建客户端，调用 CreateApp/CreateAppWithCode 创建 App；支持 --profile / --server / --code flags
app_create_test.go:  覆盖 runAppCreate 的单元测试（成功/无凭证/API错误/未知profile），用 httptest 隔离网络
app_list.go:         app list 子命令，调用 MakeService.ListResources 分页列出 org 下全部 App，tabwriter 对齐输出；支持 --profile / --server / --page / --size flags
app_list_test.go:    覆盖 runAppList 的单元测试（成功/空列表/分页JSON/无凭证/API错误/非法页码），用 httptest 隔离网络
app_init.go:         app init 子命令，在已有 Folder 内创建 provider 对应配置文件（anthropic→CLAUDE.md / openai→AGENTS.md / google→GEMINI.md / cursor→.cursorrules）
app_init_test.go:    覆盖 runAppInit 的文件系统测试（含全 provider 覆盖）
app_delete.go:          app delete 子命令，调用 Meta Server API（MakeService.DeleteResource）删除指定 App；支持 --profile 和 --server flags
app_delete_test.go:     覆盖 runAppDelete 的单元测试（成功/无凭证/API错误/未知profile），用 httptest 隔离网络
entity.go:              entity 命令组，挂载 create / delete / list 子命令
entity_create.go:       entity create 子命令，校验 field name 不以 _ 开头，支持 --app（必选）/ --json / --profile / --server；loadFields 从 JSON 文件加载字段定义
entity_create_test.go:  覆盖 runEntityCreate 的单元测试（成功/带fields/underscore校验/无凭证/API错误/未知profile/非法JSON），用 httptest 隔离网络
entity_delete.go:        entity delete 子命令，调用 Meta Server API（MakeService.DeleteResource）删除指定 Entity；支持 --app（必选）/ --profile / --server
entity_delete_test.go:   覆盖 runEntityDelete 的单元测试（成功/无凭证/API错误/未知profile），用 httptest 隔离网络
entity_list.go:         entity list 子命令，无 arg 时分页列出 app 下全部 entity（NAME/VERSION），有 arg 时显示指定 entity 详情（name/app/version + fields 表格）；支持 --app（必选）/ --profile / --server / --page / --size
entity_list_test.go:    覆盖 runEntityList 的单元测试（列表/空列表/具体entity/无字段/无凭证/API错误/未知profile），用 httptest 隔离网络
relation.go:                relation 命令组，挂载 create / update / delete / list 子命令
relation_create.go:         relation create 子命令，从 JSON 文件加载 from/to，调用 Meta Server API 创建 Relation；loadRelationProperties 从 JSON 文件加载关系属性；支持 --app（必选）/ --json（必选）/ --profile
relation_create_test.go:    覆盖 runRelationCreate 的单元测试（成功/无凭证/API错误/未知profile/非法JSON/文件不存在），用 httptest 隔离网络
relation_update.go:         relation update 子命令，从 JSON 文件加载 from/to，调用 Meta Server API 更新 Relation；支持 --app（必选）/ --json（必选）/ --profile
relation_update_test.go:    覆盖 runRelationUpdate 的单元测试（成功/无凭证/API错误/未知profile/非法JSON），用 httptest 隔离网络
relation_delete.go:         relation delete 子命令，调用 Meta Server API 删除指定 Relation；支持 --app（必选）/ --profile
relation_delete_test.go:    覆盖 runRelationDelete 的单元测试（成功/无凭证/API错误/未知profile），用 httptest 隔离网络
relation_list.go:           relation list 子命令，无 arg 时分页列出 app 下全部 relation（NAME/FROM/TO/VERSION），有 arg 时显示指定 relation 详情；支持 --app（必选）/ --profile / --page / --size / --output
relation_list_test.go:      覆盖 runRelationList 的单元测试（列表/空列表/JSON列表/详情/JSON详情/无凭证/API错误/未知profile/非法页码/非法格式），用 httptest 隔离网络
record.go:                  record 命令组，挂载 create / get / update / delete / list 子命令，--app 和 --entity 参数为子命令继承
record_create.go:           record create 子命令，从 JSON 文件加载动态 KV 数据，调用 Data Service API 创建 Record，输出 recordID；loadRecordData 从 JSON 文件加载记录数据；支持 --app（继承）/ --entity（继承）/ --json（必选）/ --profile
record_create_test.go:      覆盖 runRecordCreate 的单元测试（成功/无凭证/API错误/未知profile/非法JSON/文件不存在），用 httptest 隔离网络
record_get.go:              record get 子命令，获取单条 Record 并按 key 排序逐行输出或 JSON 格式展示；支持 --app（继承）/ --entity（继承）/ --profile / --output
record_get_test.go:         覆盖 runRecordGet 的单元测试（成功/JSON输出/无凭证/API错误/未知profile/非法格式），用 httptest 隔离网络
record_update.go:           record update 子命令，透明路由——1 个 recordID 走 /data/v1/record 单条更新，N 个走 /data/v1/field 批量更新；支持 --app（继承）/ --entity（继承）/ --json（必选）/ --profile
record_update_test.go:      覆盖 runRecordUpdate 的单元测试（单条路由验证/批量路由验证/无凭证/API错误/未知profile/非法JSON），用 httptest 隔离网络，重点验证请求路径
record_delete.go:           record delete 子命令，批量删除 Record，汇报每条记录的删除结果；支持 --app（继承）/ --entity（继承）/ --profile
record_delete_test.go:      覆盖 runRecordDelete 的单元测试（单条/批量/部分失败/无凭证/API错误/未知profile），用 httptest 隔离网络
record_list.go:             record list 子命令，分页查询 Record，自动从首条记录提取列名或使用 --fields 指定列，parseSortSpec 解析排序说明，extractKeys 提取 map 键；支持 --app（继承）/ --entity（继承）/ --profile / --page / --size / --output / --fields / --sort
record_list_test.go:        覆盖 runRecordList 的单元测试（表格/JSON/空列表/无凭证/API错误/未知profile/非法页码/非法格式/非法排序），用 httptest 隔离网络
apply.go:            apply 子命令，从 YAML 文件/目录批量应用资源（create-or-update 语义：App 不存在则创建/已存在则跳过，Entity 不存在则创建/已存在则调用 UpdateResource）；支持多文档 YAML 和目录扫描；支持 --profile / --server
apply_test.go:       apply 子命令的单元测试，覆盖单文件、多文档、目录扫描、错误场景
diff.go:             diff 子命令，对比远端 Meta Server 上的 App DSL 与本地 YAML 文件的差异；App name 从 YAML 自动推断（Make.App name 或 Entity app 字段）；分页获取全部远端 Entity，按 name 匹配后逐字段比对 type/properties；支持 -f（必选）/ --profile / --server / --output；退出码 0=无差异 1=有差异
diff_test.go:        覆盖 diff 子命令核心逻辑的单元测试（computeDiff/fetchAllEntities/jsonDeepEqual/runDiff 错误路径），用 httptest 隔离网络
output.go:           list 命令通用输出辅助（table|json 格式校验 + JSON 编码），被 app list / entity list / relation list / record list / record get 复用
stdout_test.go:      测试基础设施，提供 captureStdout 辅助函数劫持 os.Stdout 捕获输出，被各子命令测试复用
update.go:           update 子命令，从 GitHub Releases 自更新二进制；直接 import internal/build 读取版本，委托 internal/update 执行检查和替换

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
