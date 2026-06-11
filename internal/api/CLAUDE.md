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

## 成员清单
client.go:      Make Meta Service 的 HTTP 客户端，提供 ErrNotFound 哨兵、Client 类型（含 debug/headers 字段）、Option 函数选项类型、WithDebug/WithHeaders 选项、New(baseURL, token, ...Option) 构造函数、App{Key,Name,Type,Meta,Properties} / Field{Key,Name,Type,Meta,Properties,Validations} / Entity{Key,Name,Type,AppKey,Meta,Properties} / EntityProperties / RelationEnd{EntityKey,Cardinality} / RelationProperties / Relation{Key,Name,Type,AppKey,Meta,Properties} / Schema 类型、CreateApp(key, name, properties) / ListApps(page, size, filter) / DeleteApp(key) / GetApp(key) / CreateEntity(key, name, appKey, fields) / ListEntities(appKey, page, size, filter) / GetEntity(appKey, key) / UpdateEntity(key, name, appKey, fields) / DeleteEntity(key, appKey) / CreateRelation(key, name, appKey, props) / UpdateRelation / ListRelations(appKey, ...) / GetRelation(appKey, key) / DeleteRelation(key, appKey) / GetSchema(appKey) 方法；Get* 经 checkGetResult 收敛存在性，不存在返回 ErrNotFound，其余错误原样返回；do() 为底层 POST，支持 debug 输出 curl 命令 + 自定义 headers 注入，post() 处理写操作；writeResource 收口 Entity/Relation 的 Create/Update 四个写方法（仅 verb/type/path/properties 不同），metaVersion 常量统一 DSL 版本号
client_test.go: 覆盖 CreateApp / DeleteApp / ListApps / WithHeaders / WithDebug / GetApp / GetEntity / GetRelation 的单元测试（成功/API错误/空列表/格式错误/自定义头/调试模式 + not-found 业务码/200空data/500/传输错误/解码错误 的 ErrNotFound 区分），用 httptest 隔离网络
record.go:      Data Service 的 Record CRUD 层，提供 DeleteRecordResult / SortField{FieldKey,Order} / ListRecordOpts{Fields,Sort,Filter,Page,Size} 类型、CreateRecord(appKey, entityKey, data) / GetRecord(appKey, entityKey, recordID) / UpdateRecord / UpdateRecordsBatch / DeleteRecords / ListRecords 方法；请求体使用 appKey/entityKey/fieldKey 标识符；ListRecords 的 Filter 为 CEL 文本，非空时包成 filter.expression 发送；UpdateRecordsBatch 走 /data/v1/field 端点，其余走 /data/v1/record
record_test.go: 覆盖 CreateRecord / GetRecord / UpdateRecord / UpdateRecordsBatch / DeleteRecords / ListRecords 的单元测试（成功/API错误/可选参数/批量路径验证），用 httptest 隔离网络
integration.go:      Integration 服务调用层，提供 OCROptions 类型 + Client.OCR(filename, reader, opts) 方法；multipart 上传 file/business_id/verify_vat 到 /integration/v1/ocr；6 个 query 参数（coord_restore/specific_pages/crop_complete_image/crop_value_image/merge_digital_elec_invoice/return_ppi）通过 OCROptions.queryString 序列化；data 字段以 map[string]any 透传给上层渲染
integration_test.go: 覆盖 Client.OCR 的单元测试（默认请求/business_id+verify_vat 三态/query 参数序列化/API 错误/传输错误），用 httptest 隔离网络

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
