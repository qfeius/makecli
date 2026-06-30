# internal/api/
> L2 | 父级: /CLAUDE.md

## 资源标识符语义
所有资源（App / Entity / Field / Relation）采用 Key + Name 双轨制：
- **Key**: 英文字母 / 数字 / 下划线，长度 2-20，不可以下划线开头，创建后不可修改，用作唯一标识符
- **Name**: 必填的展示名，允许中英文数字下划线，可修改
- 父级引用通过 AppKey（Entity/Relation → App）、EntityKey（RelationEnd → Entity）、RelationKey/TargetFieldKey 等 *Key 字段实现

## ErrNotFound 哨兵契约
- `ErrNotFound = errors.New("资源不存在")` 是包级导出哨兵，用 `errors.Is(err, api.ErrNotFound)` 判定
- `GetApp / GetEntity / GetRelation` 仅在资源"确实不存在"时返回（用 `%w` 包裹）：业务码 404，或 code==200 且 data.key 为空（服务端软空响应约定）
- 传输错误 / 非 404 的非 200 业务码 / 解码失败 → 原样返回，**绝不**映射为 ErrNotFound
- 设计意图：把"不存在"语义收口到单一哨兵，让 apply 的 create-or-update 决策摆脱 `Key != ""` 启发式，杜绝把瞬时/传输故障误判为"不存在"而误建重复资源或把 update 降级为 create
- `checkGetResult(code, msg, dataKey)` 是三态收敛 helper（存在/不存在→ErrNotFound/真实错误）

## ErrAuthFailed 哨兵契约
- `ErrAuthFailed = errors.New("鉴权失败")` 是包级导出哨兵，用 `errors.Is(err, api.ErrAuthFailed)` 判定
- 后端走 HTTP 200 包 `{code,msg}`，拿不到 401 → 只能靠**业务码**识别：`authFailedCode = 990300403`（已知 token 验证失败码，后端无公开码表，有新鉴权码段在此扩展）
- 识别点：`do()` 用轻量探针读 code（覆盖 Meta/Data/Repo 全部走 do 的方法）；`integration.go` 的 OCR 自发请求，单独以 `result.Code` 判定。两处共用 `authFailedErr(code,msg)` 单一构造点（`%w` 包裹哨兵 + 保留原始 code/msg 供上层展示）
- 设计意图：把横切的"鉴权失败"语义收口到单一哨兵，api 层只报事实、不管呈现；cmd 层（errors.go reportExecuteError）用 errors.Is 统一翻译成 `makecli login` 引导。与 ErrNotFound 同为"事实 vs 呈现"的分层边界

## UniqueConstraintError 类型化错误契约
- `UniqueConstraintError{Constraint, Fields, Message}` 是包级导出错误类型，用 `errors.As(err, &api.UniqueConstraintError{})` 判定；表示写入 Record 违反 Entity 唯一性约束
- 后端走 HTTP 200 包 `{code,msg,data}`，唯一冲突业务码 `conflictCode = 409`，`data` 回传冲突的约束 `constraint` 与参与字段 `fields`
- 收口：`conflictData{constraint,fields}` 承载响应形态，`writeStatusErr(code,msg,conflict)` 单一翻译器——409 → UniqueConstraintError（携带约束名/字段），其余非 200 → 通用「API 错误」；顺带收口原本散落各写方法的非 200 翻译重复
- 识别点：`post()`（覆盖 UpdateRecord/UpdateRecordsBatch 及所有写）+ `CreateRecord`（走 do，data 嵌入 conflictData）。Error() 串自解释（`唯一性约束冲突 [name]：字段 (...) 已存在相同值`），cmd 层无需特判，照常打印
- 设计意图：与 ErrNotFound / ErrAuthFailed 同为「api 报事实、cmd 管呈现」的分层边界

## 成员清单
client.go:      Make Meta Service 的 HTTP 客户端，提供 ErrNotFound / ErrAuthFailed 哨兵（ErrAuthFailed 经 authFailedErr 构造、authFailedCode=990300403）、UniqueConstraintError 类型化错误（409 唯一性冲突，经 writeStatusErr 构造、conflictCode=409）、Client 类型（含 debug/dryRun/headers 字段）、Option 函数选项类型、WithDebug/WithHeaders/WithDryRun 选项、New(baseURL, token, ...Option) 构造函数、App{Key,Name,Type,Meta,Properties} / Field{Key,Name,Type,Meta,Properties,Validations} / Entity{Key,Name,Type,AppKey,Meta,Properties} / EntityProperties{Fields,UniqueConstraints} / UniqueConstraint{Name,Fields}（Entity 级唯一性约束，底层=唯一索引；omitempty）/ RelationEnd{EntityKey,Cardinality} / RelationProperties / Relation{Key,Name,Type,AppKey,Meta,Properties} / Schema 类型、CreateApp(key, name, properties) / ListApps(page, size, filter) / DeleteApp(key) / GetApp(key) / CreateEntity(key, name, appKey, props EntityProperties) / ListEntities(appKey, page, size, filter) / GetEntity(appKey, key) / UpdateEntity(key, name, appKey, props EntityProperties) / DeleteEntity(key, appKey) / CreateRelation(key, name, appKey, props) / UpdateRelation / ListRelations(appKey, ...) / GetRelation(appKey, key) / DeleteRelation(key, appKey) / GetSchema(appKey) 方法（Entity 写收 EntityProperties 与 Relation 写收 RelationProperties 对称，消除「只发 fields」特例）；Get* 经 checkGetResult 收敛存在性，不存在返回 ErrNotFound，其余错误原样返回；do() 为底层 POST，支持 debug 输出 curl 命令 + 自定义 headers 注入 + 每请求注入 Traceparent/X-Log-Id（internal/trace，trace-id 全程稳定、parent-id 每请求新生成）+ dryRun 时注入横切信号 X-Dry-Run: true（WithDryRun 开启，服务端跑真实流程但 ROLLBACK 不落库；响应结构不变，调用方仍按 code 判定）+ 解码 result 前以轻量探针读 code、命中 authFailedCode 即抛 ErrAuthFailed（横切鉴权识别收口于此），post() 处理写操作（解码 409 conflictData，非 200 经 writeStatusErr 翻译）；writeResource 收口 Entity/Relation 的 Create/Update 四个写方法（仅 verb/type/path/properties 不同），metaVersion 常量统一 DSL 版本号
client_test.go: 覆盖 CreateApp / DeleteApp / ListApps / WithHeaders / WithDebug / GetApp / GetEntity / GetRelation / TraceHeaders 的单元测试（成功/API错误/空列表/格式错误/自定义头/调试模式 + not-found 业务码/200空data/500/传输错误/解码错误 的 ErrNotFound 区分 + 出站 Traceparent 格式与 X-Log-Id==trace-id 段一致），用 httptest 隔离网络
repository.go:  代码仓库服务（make-repo）调用层，提供 CodeRepo / CodeRepoEnv / CodeRepoMeta / CodeRepoProperties / CodeRepoResource 类型、CreateRepository(appKey) 方法（POST /code/v1/repository + X-Make-Target: MakeService.CreateResource，body {type: Make.Code.Repository, appKey}，幂等准备租户 Organization 与 preview/production 双环境私有仓库）；CodeRepoResource.CloneURLFor(env) 把三种响应形态收口为单一查询（properties.env.<env> → meta.repositories[].environment → meta.cloneUrl 单仓库兼容），找不到返回空串
repository_test.go: 覆盖 CreateRepository（请求体/Header/路径/双环境解析/API错误/传输错误）与 CloneURLFor 三级回退的单元测试，用 httptest 隔离网络
record.go:      Data Service 的 Record CRUD 层，提供 DeleteRecordResult / SortField{FieldKey,Order} / ListRecordOpts{Fields,Sort,Filter,Page,Size} 类型、CreateRecord(appKey, entityKey, data) / GetRecord(appKey, entityKey, recordID) / UpdateRecord / UpdateRecordsBatch / DeleteRecords / ListRecords 方法；请求体使用 appKey/entityKey/fieldKey 标识符；ListRecords 的 Filter 为 CEL 文本，非空时包成 filter.expression 发送；UpdateRecordsBatch 走 /data/v1/field 端点，其余走 /data/v1/record；写方法（Create/Update/UpdateBatch）违反唯一性约束时返回 UniqueConstraintError（CreateRecord 走 do、data 嵌入 conflictData；Update/UpdateBatch 走 post，409 翻译收口于 client.go writeStatusErr）
record_test.go: 覆盖 CreateRecord / GetRecord / UpdateRecord / UpdateRecordsBatch / DeleteRecords / ListRecords 的单元测试（成功/API错误/可选参数/批量路径验证 + 409 唯一性冲突经 do/post 两路均返回 UniqueConstraintError），用 httptest 隔离网络
integration.go:      Integration 服务调用层，提供 OCROptions 类型 + Client.OCR(filename, reader, opts) 方法；multipart 上传 file/business_id/verify_vat 到 /integration/v1/ocr；与 do() 同源注入 Traceparent/X-Log-Id（internal/trace）；自发请求（不走 do），故以 result.Code==authFailedCode 单独判定抛 ErrAuthFailed（与 do 共用 authFailedErr 构造）；6 个 query 参数（coord_restore/specific_pages/crop_complete_image/crop_value_image/merge_digital_elec_invoice/return_ppi）通过 OCROptions.queryString 序列化；data 字段以 map[string]any 透传给上层渲染
integration_test.go: 覆盖 Client.OCR 的单元测试（默认请求/business_id+verify_vat 三态/query 参数序列化/API 错误/传输错误），用 httptest 隔离网络

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
