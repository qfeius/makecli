# cmd/
> L2 | 父级: /CLAUDE.md

## 渲染约定
- 表格统一用 `github.com/olekukonko/tablewriter`（边框 + Header），禁止用 stdlib `text/tabwriter`
- key-value 头部信息（File / Name / App 等）用 `fmt.Printf("%-N s %s\n", ...)` 平铺，不进表格
- 写新输出前先 grep 邻居命令的渲染方式再动手，避免风格漂移

## 全局标志（root PersistentFlags）
- `--profile string`（default "default"）— 凭证 profile 名，绑定全局变量 `cmd.Profile`（包级 `var Profile = "default"`，单测无需 cobra 解析也能用）
- `--server-url string` — Meta Server 基础 URL，覆盖 profile config 与环境 preset，绑定 `cmd.ServerURL`
- `--repo-server-url string` — 代码仓库服务（make-gitea）基础 URL，覆盖 profile config 与环境 preset，绑定 `cmd.RepoServerURL`
- `--env string` — 后端环境（dev/test/production），覆盖 `[settings] environment`，绑定 `cmd.Environment`；空串=回退 settings 或默认 dev。后端 URL 三件套兜底来自 `config.Environment` preset（解析链 flag > profile config > 环境 preset，见 client.go resolveEnvironment）
- `--debug`（隐藏）— 输出 curl 调试信息，绑定 `cmd.DebugMode`

三者都不再从 `runXxx` 签名穿越，统一由 `newClientFromProfile()`（零参数）在内部直接读取全局。新增子命令时：
- 禁止声明本地 `--profile`
- `runXxx` 函数不要带 profile 参数
- 客户端构建调用 `newClientFromProfile()` 即可，不传任何参数
- 单测需要切换 profile 时用 `setProfile(t, "name")`（stdout_test.go），t.Cleanup 自动还原

## 成员清单
root.go:             根命令入口，挂载所有顶级子命令（schema / apply / diff / update / integration / preflight 等；deploy 已下沉为 app 子命令），对外暴露 Execute(version, date)；定义全局 PersistentFlag --profile / --server-url / --repo-server-url / --env / --debug，分别绑定全局变量 Profile / ServerURL / RepoServerURL / Environment / DebugMode；后端 URL 兜底交给 config.Environment preset（不再持 default*Server 常量）；钩入 notifier.Start/Finish 生命周期；包内 commandName 解析顶级命令名
root_test.go:        覆盖 commandName 顶级命令解析的单元测试（version/version list/update/app create/空 args/未知命令）
version.go:          version 子命令组，默认 Run 打印当前版本（参考 GitHub CLI 模式），挂载 list 子命令
version_test.go:     覆盖 formatVersion / changelogURL 的纯函数测试
version_list.go:     version list 子命令，调 internal/update.ListReleases 拉取 GitHub 最近 N 条 release，tablewriter 输出 CURRENT/VERSION/PUBLISHED/URL（JSON 输出保留 name 字段）；CURRENT 列对比 build.Version 标记当前安装版本；支持 --limit（默认20，1-100）/ --output（table|json）
version_list_test.go: 覆盖 runVersionList 的单元测试（table 渲染 / CURRENT 标记 / JSON 输出去除 assets / 空列表 / 非法 limit / 非法 output / API 错误 / DEV 不打标记），用 httptest 隔离网络
configure.go:        configure 命令组（无本地标志，复用全局 --profile），默认行为等同 token 子命令；子命令: token（交互写 ~/.make/credentials）/ config（交互写 ~/.make/config）/ set（直接写单个 key）/ get（读取单个 key）/ verify（在线验证 token）；validateConfigKey 校验合法 profile key 集合（server-url / repo-server-url / auth-server-url / X-Tenant-ID / X-Operator-ID）；特殊键 environment 经 setEnvironment 路由到全局 [settings]（非 profile，值校验 ∈ dev/test/production，写 config.SetSetting），get environment 读 settings 缺省回退 dev
login.go:            login 顶级命令，浏览器 OAuth 登陆（复用全局 --profile，本地 --timeout 默认3m / --no-open-browser）；runLogin 编排 discover→起动态端口回调 server→每次新注册 client（RFC 7591）→PKCE→开浏览器→等回调→换 token，仅把 access_token 写入 ~/.make/credentials[Profile]；make preset（business_type=make / scopes=make:resources）；OAuth 元数据地址由身份服务器基址拼 .well-known 路径（authMetadataURL），基址取 profile 的 auth-server-url、缺省回退当前环境 preset 的 AuthServerURL（resolveEnvironment），使 token 颁发方与后端环境对齐；openBrowserFunc 包级可打桩变量
login_test.go:       覆盖 runLogin 的单元测试（成功写 token / 写入选定 profile / --no-open-browser 打印 URL 并超时 / 缺 registration_endpoint 报错），用 httptest 模拟 OAuth 三端点 + openBrowserFunc 桩注入回调，t.Setenv 隔离凭证
configure_test.go:   覆盖 mask / validateJWT / validateConfigKey 的纯函数测试 + configure set/get environment（写读全局 [settings]、非法环境名拒绝、不受 --profile 影响）
configure_verify.go:     configure verify 子命令，加载 credentials + config，JWT 格式校验后调 ListApps 在线验证 token；输出 verifyResult（profile/valid/token/server_url/tenant_id/operator_id/message）；支持 --output table|json；valid=false 时 exit 1
configure_verify_test.go: 覆盖 runConfigureVerify 的单元测试（valid token table/json、token not configured、malformed JWT、server 401、config 字段传递、unknown profile），用 httptest 隔离网络
client.go:           公共 helper，resolveProfile() 收口凭证与配置解析，resolveEnvironment() 收口环境 preset（--env flag > [settings] environment > 默认 dev，未知名报错）；newClientFromProfile()（零参数）构建 Meta/Data 客户端，newRepoClientFromProfile() 构建代码仓库服务客户端并额外返回裸 token（供 deploy 的 git Basic 认证）；firstNonEmpty 统一「flag > profile config > 环境 preset」取值链
client_test.go:      覆盖 resolveEnvironment 优先级（默认 dev / settings 覆盖默认 / --env 覆盖 settings / 未知环境报错），setEnvFlag 临时覆盖全局 Environment
stdout_test.go:      测试基础设施，提供 captureStdout 劫持 stdout 的辅助函数 + setProfile(t, name) 临时覆盖全局 Profile（t.Cleanup 还原）
app.go:              app 命令组，挂载 create / list / init / delete / deploy 子命令；提供 loadAppManifestFromFile 共享 helper（从 YAML 加载唯一 Make.App 资源）
app_create.go:       app create 子命令，位置参数为 App key（英文标识符），--name 为展示名（支持中文；key 缺省且 name 是合法标识符时直接作 key），--description 描述；支持 -f YAML 文件模式；CreateApp(key, displayName, properties) 透传；创建成功后 prepareCodeRepos 幂等准备 preview/production 双环境代码仓库并打印 cloneUrl（仓库服务故障降级为 stderr 警告不改变退出码，deploy 会自动重试）
app_create_test.go:  覆盖 runAppCreate / runAppCreateFromFile 的单元测试（成功/无凭证/API错误/未知profile/文件模式/打印仓库地址/仓库失败降级/--name 作 key），用 httptest 隔离网络；stubRepoServer 临时指向仓库服务 mock；含 validResourceKey 通用 key 校验测试（长度 2-20，不可以下划线开头）
app_list.go:         app list 子命令，调用 MakeService.ListResources 分页列出 org 下全部 App，输出列 KEY/NAME/VERSION/CREATED AT；支持 --profile / --server / --page / --size / --filter flags；parseFilter 把 "key=value" 过滤语法翻译为 CEL 表达式文本（key 走等值 `key == 'v'`，name/description 走 `field.contains('v')`，逗号 = `||`），celString 转义单引号字面量；服务端按 Expression{expression} 解析（见 AgenticDSL/Design/ExpressionDesign.md）
app_list_test.go:    覆盖 runAppList / parseFilter 的单元测试（成功/空列表/分页JSON/过滤请求/非法过滤/无凭证/API错误/非法页码），用 httptest 隔离网络
app_init.go:         app init 子命令，在目标目录创建 CLAUDE.md 和 AGENTS.md（内容来自 agents 包 embed.FS）；folder 可选，默认当前目录，不存在则自动创建
app_init_test.go:    覆盖 runAppInit 的单元测试（创建文件/创建目录/内容匹配 embed/重复检测）
app_delete.go:          app delete 子命令，调用 Meta Server API（MakeService.DeleteResource）删除指定 App；支持 --profile / --server flags 和 -f YAML 文件模式
app_delete_test.go:     覆盖 runAppDelete / runAppDeleteFromFile 的单元测试（成功/无凭证/API错误/未知profile/文件模式），用 httptest 隔离网络
entity.go:              entity 命令组，挂载 create / delete / list 子命令；--app（appKey）参数为子命令继承
entity_create.go:       entity create 子命令，位置参数为 Entity key，--name 为展示名（缺省回退 key），--json 加载 fields；校验 field.Key 合法（validResourceKey）；--app 传 appKey；loadFields 从 JSON 文件加载字段定义
entity_create_test.go:  覆盖 runEntityCreate 的单元测试（成功/带fields/field key 校验/无凭证/API错误/未知profile/非法JSON），用 httptest 隔离网络
entity_delete.go:        entity delete 子命令，按 key 调用 Meta Server API（MakeService.DeleteResource）删除指定 Entity；--app 传 appKey
entity_delete_test.go:   覆盖 runEntityDelete 的单元测试（成功/无凭证/API错误/未知profile），用 httptest 隔离网络
entity_list.go:         entity list 子命令，按 appKey 分页列出 entity（KEY/NAME/VERSION），位置参数为 entity key 时显示详情（Key/Name/App/Version + fields 表格 KEY/NAME/TYPE）；--app 传 appKey；复用 parseFilter
entity_list_test.go:    覆盖 runEntityList 的单元测试（列表/空列表/过滤请求/具体entity/无字段/无凭证/API错误/未知profile），用 httptest 隔离网络
relation.go:                relation 命令组，挂载 create / update / delete / list 子命令；--app（appKey）参数为子命令继承
relation_create.go:         relation create 子命令，位置参数为 Relation key，--name 为展示名，--json 加载 from/to（entityKey 引用），调用 Meta Server API 创建 Relation
relation_create_test.go:    覆盖 runRelationCreate 的单元测试（成功/无凭证/API错误/未知profile/非法JSON/文件不存在），用 httptest 隔离网络
relation_update.go:         relation update 子命令，按 key 定位，--name 更新展示名，--json 加载 from/to（entityKey 引用），调用 Meta Server API 更新 Relation
relation_update_test.go:    覆盖 runRelationUpdate 的单元测试（成功/无凭证/API错误/未知profile/非法JSON），用 httptest 隔离网络
relation_delete.go:         relation delete 子命令，按 key 调用 Meta Server API 删除指定 Relation
relation_delete_test.go:    覆盖 runRelationDelete 的单元测试（成功/无凭证/API错误/未知profile），用 httptest 隔离网络
relation_list.go:           relation list 子命令，按 appKey 分页列出 relation（KEY/NAME/FROM/TO/VERSION，FROM/TO 显示 entityKey(cardinality)），位置参数为 relation key 时显示详情；复用 parseFilter
relation_list_test.go:      覆盖 runRelationList 的单元测试（列表/空列表/JSON列表/详情/JSON详情/过滤请求/无凭证/API错误/未知profile/非法页码/非法格式），用 httptest 隔离网络
record.go:                  record 命令组，挂载 create / get / update / delete / list 子命令；--app（appKey）和 --entity（entityKey）参数为子命令继承
record_create.go:           record create 子命令，从 JSON 文件加载动态 KV 数据，调用 Data Service API 创建 Record，输出 recordID；loadRecordData 从 JSON 文件加载记录数据；支持 --app（继承）/ --entity（继承）/ --json（必选）/ --profile
record_create_test.go:      覆盖 runRecordCreate 的单元测试（成功/无凭证/API错误/未知profile/非法JSON/文件不存在），用 httptest 隔离网络
record_get.go:              record get 子命令，获取单条 Record 并按 key 排序逐行输出或 JSON 格式展示；支持 --app（继承）/ --entity（继承）/ --profile / --output
record_get_test.go:         覆盖 runRecordGet 的单元测试（成功/JSON输出/无凭证/API错误/未知profile/非法格式），用 httptest 隔离网络
record_update.go:           record update 子命令，透明路由——1 个 recordID 走 /data/v1/record 单条更新，N 个走 /data/v1/field 批量更新；支持 --app（继承）/ --entity（继承）/ --json（必选）/ --profile
record_update_test.go:      覆盖 runRecordUpdate 的单元测试（单条路由验证/批量路由验证/无凭证/API错误/未知profile/非法JSON），用 httptest 隔离网络，重点验证请求路径
record_delete.go:           record delete 子命令，批量删除 Record，汇报每条记录的删除结果；支持 --app（继承）/ --entity（继承）/ --profile
record_delete_test.go:      覆盖 runRecordDelete 的单元测试（单条/批量/部分失败/无凭证/API错误/未知profile），用 httptest 隔离网络
record_list.go:             record list 子命令，分页查询 Record，自动从首条记录提取列名或使用 --fields 指定列，parseSortSpec 解析排序说明，extractKeys 提取 map 键；--filter 直收 raw CEL 表达式（不在 CLI 解析，原样塞进 filter.expression，服务端裁决合法性；命令带 Long+EXAMPLES 教 CEL 用法）；支持 --app（继承）/ --entity（继承）/ --profile / --page / --size / --output / --fields / --sort / --filter
record_list_test.go:        覆盖 runRecordList 的单元测试（表格/JSON/空列表/无凭证/API错误/未知profile/非法页码/非法格式/非法排序），用 httptest 隔离网络
deploy.go:           app deploy 子命令（`makecli app deploy`），--env preview|production（必选）选定环境后调用代码仓库服务 CreateRepository（MakeService.CreateResource，幂等）拿到对应 cloneUrl，git push 当前 HEAD 到固定分支 HEAD:refs/heads/<deployBranch>（常量 dev，webhook 约定，非用户旋钮）触发构建 webhook；--app 缺省时取 git 仓库根目录名推断 app key；--force 透传强制推送；token 经 GIT_CONFIG_* 环境变量注入 Basic 认证（make:<token>，不进程序参数避免 ps 泄露），credential.helper 置空防 keychain 介入；gitOutputFunc / gitPushFunc 包级变量便于测试打桩
deploy_test.go:      覆盖 runDeploy 的单元测试（preview/production 推送目标、--force 透传、appKey 推断与非法推断、非 git 仓库、非法 env、无凭证、API 错误、cloneUrl 缺失、push 失败），stubGit 打桩 git 交互 + httptest 隔离网络
preflight.go:        preflight 顶级子命令，校验工作目录（可选位置参数 [dir]，默认 cwd）是否具备 Make app 必需骨架——apps/dsl 目录 + apps/service/package.json + apps/ui/package.json；逐项 os.Stat 打印 ✓/✗ 清单，任一缺失或类型不符返回 errPreflightFailed（main.go 转译退出码 1，SilenceErrors 不污染 stderr），作 CI/deploy 前置门禁；requiredLayout 切片驱动统一校验，layoutEntry{path,dir} 区分目录/文件
preflight_test.go:   覆盖 runPreflight 的单元测试（完整骨架通过 / dsl 缺失 / service 缺 package.json / ui 缺 package.json / dsl 是文件非目录 / 空目录全失败），用 t.TempDir 构造真实目录树隔离文件系统
apply.go:            apply 子命令，从 YAML 文件/目录批量应用资源（create-or-update 语义：按 Key 检测存在性，App 不存在则创建/已存在则跳过，Entity/Relation 不存在则创建/已存在则更新）；存在性判定经 api.ErrNotFound 哨兵——仅"确实不存在"才创建，Get 的瞬时/传输/非 not-found 错误一律上抛不创建（杜绝误建重复资源或把 update 降级为 create）；依赖顺序 App→Entity→Relation；ResourceManifest 提供 Key（标识符）/Name（展示名）/Type/AppKey/Meta/Properties 字段；支持多文档 YAML 和目录扫描
apply_test.go:       apply 子命令的单元测试，覆盖单文件、多文档、目录扫描、Relation 创建/更新/缺 appKey 字段错误、App+Entity+Relation 混合目录场景
diff.go:             diff 子命令，对比远端 Meta Server 上的 App DSL（Entity + Relation）与本地 YAML 文件的差异；App key 从 YAML 自动推断（Make.App key 或 Entity/Relation appKey 字段）；分页获取全部远端资源，按 Key 匹配后逐字段（key）/端点（entityKey）比对；支持 -f（必选）/ --output；退出码 0=无差异 1=有差异（table 与 json 模式一致，经 errDiffFound 哨兵上抛到 main 转译，不再用 os.Exit，可单测）；命令开 SilenceErrors，由 reportDiffError 亲自打印真实错误、放过 errDiffFound 哨兵不污染 stderr
diff_test.go:        覆盖 diff 子命令核心逻辑的单元测试（computeDiff/computeRelationDiff/fetchAllEntities/fetchAllRelations/jsonDeepEqual/runDiff 错误路径），用 httptest 隔离网络
schema.go:           schema 顶级子命令，按 appKey 调用 MakeService.GetResource 获取聚合 Schema（App + Entities + Relations），JSON 输出
schema_test.go:      覆盖 runSchema 的单元测试（成功/无凭证/API错误/未知profile），用 httptest 隔离网络
output.go:           list 命令通用输出辅助（table|json 格式校验 + JSON 编码），被 app list / entity list / relation list / record list / record get 复用
update.go:           update 子命令，支持 [version] 位置参数（v0.2.0 或 0.2.0）和 --force 标志；无 arg 走 CheckLatest 流程，指定版本走 GetRelease；CompareVersions 决定 upgrade/same/downgrade 分支，降级需 --force；DEV 版本跳过比较；applyFunc 包级变量便于测试打桩
update_test.go:      覆盖 runUpdate 的单元测试（latest 已到位/有更新、specific 升级/同版本/降级拒绝/--force 降级/规范化无 v 前缀/非法 semver/tag 不存在/DEV 跳过比较），applyFunc 打桩避免真实替换二进制
integration.go:           integration 命令组，挂载 ocr 子命令；预留扩展点供未来其它 integration（translate / asr / embed 等）
integration_ocr.go:       integration ocr 子命令，校验文件后缀（.pdf/.ofd/.png/.jpg/.jpeg）后通过 newClientFromProfile 上传，调用 api.OCR；renderOCRTable 风格对齐 entity list <name>：顶部 File/Bills/Took 头部 + 每张票一个 tablewriter LABEL/VALUE 边框表格，按 spec sample 解析 result.pages[].bills[].items[]（过滤空值），断言失败回退 JSON；支持 -f|--file（必选）/ --profile / --output（table|json）/ --business-id / --verify-vat（默认 true，仅 Changed 时显式发送）/ --coord-restore-original / --pages / --crop-complete / --crop-value / --merge-elec / --return-ppi
integration_ocr_test.go:  覆盖 runIntegrationOCR / renderOCRTable 的单元测试（table 渲染 spec sample / json 输出 / 非法扩展名 / 非法格式 / 文件不存在 / 无凭证 / 未知 profile / API 错误 / 异常结构回退），用 httptest 隔离网络

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
