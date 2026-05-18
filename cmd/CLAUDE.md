# cmd/
> L2 | 父级: /CLAUDE.md

## 渲染约定
- 表格统一用 `github.com/olekukonko/tablewriter`（边框 + Header），禁止用 stdlib `text/tabwriter`
- key-value 头部信息（File / Name / App 等）用 `fmt.Printf("%-N s %s\n", ...)` 平铺，不进表格
- 写新输出前先 grep 邻居命令的渲染方式再动手，避免风格漂移

## 全局标志（root PersistentFlags）
- `--profile string`（default "default"）— 凭证 profile 名，绑定全局变量 `cmd.Profile`（包级 `var Profile = "default"`，单测无需 cobra 解析也能用）
- `--server-url string` — Meta Server 基础 URL，覆盖 config 与默认值，绑定 `cmd.ServerURL`
- `--debug`（隐藏）— 输出 curl 调试信息，绑定 `cmd.DebugMode`

三者都不再从 `runXxx` 签名穿越，统一由 `newClientFromProfile()`（零参数）在内部直接读取全局。新增子命令时：
- 禁止声明本地 `--profile`
- `runXxx` 函数不要带 profile 参数
- 客户端构建调用 `newClientFromProfile()` 即可，不传任何参数
- 单测需要切换 profile 时用 `setProfile(t, "name")`（stdout_test.go），t.Cleanup 自动还原

## 成员清单
root.go:             根命令入口，挂载所有子命令（含 schema），对外暴露 Execute(version, date)；定义全局 PersistentFlag --profile / --server-url / --debug，分别绑定全局变量 Profile / ServerURL / DebugMode
version.go:          version 子命令组，默认 Run 打印当前版本（参考 GitHub CLI 模式），挂载 list 子命令
version_test.go:     覆盖 formatVersion / changelogURL 的纯函数测试
version_list.go:     version list 子命令，调 internal/update.ListReleases 拉取 GitHub 最近 N 条 release，tablewriter 输出 CURRENT/VERSION/PUBLISHED/NAME/URL；CURRENT 列对比 build.Version 标记当前安装版本；支持 --limit（默认20，1-100）/ --output（table|json）
version_list_test.go: 覆盖 runVersionList 的单元测试（table 渲染 / CURRENT 标记 / JSON 输出去除 assets / 空列表 / 非法 limit / 非法 output / API 错误 / 空 Name 回退 / DEV 不打标记），用 httptest 隔离网络
configure.go:        configure 命令组（无本地标志，复用全局 --profile），默认行为等同 token 子命令；子命令: token（交互写 ~/.make/credentials）/ config（交互写 ~/.make/config）/ set（直接写单个 key）/ get（读取单个 key）/ verify（在线验证 token）；validateConfigKey 校验合法 key 集合
configure_test.go:   覆盖 mask / validateJWT / validateConfigKey 的纯函数测试
configure_verify.go:     configure verify 子命令，加载 credentials + config，JWT 格式校验后调 ListApps 在线验证 token；输出 verifyResult（profile/valid/token/server_url/tenant_id/operator_id/message）；支持 --output table|json；valid=false 时 exit 1
configure_verify_test.go: 覆盖 runConfigureVerify 的单元测试（valid token table/json、token not configured、malformed JWT、server 401、config 字段传递、unknown profile），用 httptest 隔离网络
client.go:           公共 helper，newClientFromProfile()（零参数）从全局 Profile/ServerURL/DebugMode 构建 API 客户端，注入 debug/headers 选项
stdout_test.go:      测试基础设施，提供 captureStdout 劫持 stdout 的辅助函数 + setProfile(t, name) 临时覆盖全局 Profile（t.Cleanup 还原）
app.go:              app 命令组，挂载 app 相关子命令；提供 loadAppManifestFromFile 共享 helper（从 YAML 加载唯一 Make.App 资源）
app_create.go:       app create 子命令，通过 newClientFromProfile 构建客户端，调用 CreateApp 创建 App；支持 --description / --render-name 本地 flag 和 -f YAML 文件模式（凭证走全局 --profile）；render-name 默认与 name 一致
app_create_test.go:  覆盖 runAppCreate / runAppCreateFromFile 的单元测试（成功/无凭证/API错误/未知profile/文件模式），用 httptest 隔离网络
app_list.go:         app list 子命令，调用 MakeService.ListResources 分页列出 org 下全部 App，tabwriter 对齐输出；支持 --profile / --server / --page / --size / --filter flags；parseFilter 解析 "key=value" 过滤表达式，name 字段自动转 contains 模糊匹配
app_list_test.go:    覆盖 runAppList / parseFilter 的单元测试（成功/空列表/分页JSON/过滤请求/非法过滤/无凭证/API错误/非法页码），用 httptest 隔离网络
app_init.go:         app init 子命令，在目标目录创建 CLAUDE.md 和 AGENTS.md（内容来自 agents 包 embed.FS）；folder 可选，默认当前目录，不存在则自动创建
app_init_test.go:    覆盖 runAppInit 的单元测试（创建文件/创建目录/内容匹配 embed/重复检测）
app_delete.go:          app delete 子命令，调用 Meta Server API（MakeService.DeleteResource）删除指定 App；支持 --profile / --server flags 和 -f YAML 文件模式
app_delete_test.go:     覆盖 runAppDelete / runAppDeleteFromFile 的单元测试（成功/无凭证/API错误/未知profile/文件模式），用 httptest 隔离网络
entity.go:              entity 命令组，挂载 create / delete / list 子命令
entity_create.go:       entity create 子命令，校验 field name 不以 _ 开头，支持 --app（必选）/ --json / --profile / --server；loadFields 从 JSON 文件加载字段定义
entity_create_test.go:  覆盖 runEntityCreate 的单元测试（成功/带fields/underscore校验/无凭证/API错误/未知profile/非法JSON），用 httptest 隔离网络
entity_delete.go:        entity delete 子命令，调用 Meta Server API（MakeService.DeleteResource）删除指定 Entity；支持 --app（必选）/ --profile / --server
entity_delete_test.go:   覆盖 runEntityDelete 的单元测试（成功/无凭证/API错误/未知profile），用 httptest 隔离网络
entity_list.go:         entity list 子命令，无 arg 时分页列出 app 下全部 entity（NAME/VERSION），有 arg 时显示指定 entity 详情（name/app/version + fields 表格）；支持 --app（必选）/ --profile / --server / --page / --size / --filter；复用 parseFilter 解析过滤表达式
entity_list_test.go:    覆盖 runEntityList 的单元测试（列表/空列表/过滤请求/具体entity/无字段/无凭证/API错误/未知profile），用 httptest 隔离网络
relation.go:                relation 命令组，挂载 create / update / delete / list 子命令
relation_create.go:         relation create 子命令，从 JSON 文件加载 from/to，调用 Meta Server API 创建 Relation；loadRelationProperties 从 JSON 文件加载关系属性；支持 --app（必选）/ --json（必选）/ --profile
relation_create_test.go:    覆盖 runRelationCreate 的单元测试（成功/无凭证/API错误/未知profile/非法JSON/文件不存在），用 httptest 隔离网络
relation_update.go:         relation update 子命令，从 JSON 文件加载 from/to，调用 Meta Server API 更新 Relation；支持 --app（必选）/ --json（必选）/ --profile
relation_update_test.go:    覆盖 runRelationUpdate 的单元测试（成功/无凭证/API错误/未知profile/非法JSON），用 httptest 隔离网络
relation_delete.go:         relation delete 子命令，调用 Meta Server API 删除指定 Relation；支持 --app（必选）/ --profile
relation_delete_test.go:    覆盖 runRelationDelete 的单元测试（成功/无凭证/API错误/未知profile），用 httptest 隔离网络
relation_list.go:           relation list 子命令，无 arg 时分页列出 app 下全部 relation（NAME/FROM/TO/VERSION），有 arg 时显示指定 relation 详情；支持 --app（必选）/ --profile / --page / --size / --output / --filter；复用 parseFilter 解析过滤表达式
relation_list_test.go:      覆盖 runRelationList 的单元测试（列表/空列表/JSON列表/详情/JSON详情/过滤请求/无凭证/API错误/未知profile/非法页码/非法格式），用 httptest 隔离网络
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
apply.go:            apply 子命令，从 YAML 文件/目录批量应用资源（create-or-update 语义：App 不存在则创建/已存在则跳过，Entity/Relation 不存在则创建/已存在则更新）；依赖顺序 App→Entity→Relation；支持多文档 YAML 和目录扫描；支持 --profile / --server
apply_test.go:       apply 子命令的单元测试，覆盖单文件、多文档、目录扫描、Relation 创建/更新/缺字段错误、App+Entity+Relation 混合目录场景
diff.go:             diff 子命令，对比远端 Meta Server 上的 App DSL（Entity + Relation）与本地 YAML 文件的差异；App name 从 YAML 自动推断（Make.App name 或 Entity/Relation app 字段）；分页获取全部远端 Entity 和 Relation，按 name 匹配后逐字段/端点比对；支持 -f（必选）/ --profile / --server / --output；退出码 0=无差异 1=有差异
diff_test.go:        覆盖 diff 子命令核心逻辑的单元测试（computeDiff/computeRelationDiff/fetchAllEntities/fetchAllRelations/jsonDeepEqual/runDiff 错误路径），用 httptest 隔离网络
schema.go:           schema 顶级子命令，调用 MakeService.GetResource 获取指定 App 的聚合 Schema（App + Entities + Relations），JSON 输出；支持 --app（必选）/ --profile
schema_test.go:      覆盖 runSchema 的单元测试（成功/无凭证/API错误/未知profile），用 httptest 隔离网络
output.go:           list 命令通用输出辅助（table|json 格式校验 + JSON 编码），被 app list / entity list / relation list / record list / record get 复用
update.go:           update 子命令，支持 [version] 位置参数（v0.2.0 或 0.2.0）和 --force 标志；无 arg 走 CheckLatest 流程，指定版本走 GetRelease；CompareVersions 决定 upgrade/same/downgrade 分支，降级需 --force；DEV 版本跳过比较；applyFunc 包级变量便于测试打桩
update_test.go:      覆盖 runUpdate 的单元测试（latest 已到位/有更新、specific 升级/同版本/降级拒绝/--force 降级/规范化无 v 前缀/非法 semver/tag 不存在/DEV 跳过比较），applyFunc 打桩避免真实替换二进制
integration.go:           integration 命令组，挂载 ocr 子命令；预留扩展点供未来其它 integration（translate / asr / embed 等）
integration_ocr.go:       integration ocr 子命令，校验文件后缀（.pdf/.ofd/.png/.jpg/.jpeg）后通过 newClientFromProfile 上传，调用 api.OCR；renderOCRTable 风格对齐 entity list <name>：顶部 File/Bills/Took 头部 + 每张票一个 tablewriter LABEL/VALUE 边框表格，按 spec sample 解析 result.pages[].bills[].items[]（过滤空值），断言失败回退 JSON；支持 -f|--file（必选）/ --profile / --output（table|json）/ --business-id / --verify-vat（默认 true，仅 Changed 时显式发送）/ --coord-restore-original / --pages / --crop-complete / --crop-value / --merge-elec / --return-ppi
integration_ocr_test.go:  覆盖 runIntegrationOCR / renderOCRTable 的单元测试（table 渲染 spec sample / json 输出 / 非法扩展名 / 非法格式 / 文件不存在 / 无凭证 / 未知 profile / API 错误 / 异常结构回退），用 httptest 隔离网络

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
