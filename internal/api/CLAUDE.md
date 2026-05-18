# internal/api/
> L2 | 父级: /CLAUDE.md

## 资源标识符语义
所有资源（App / Entity / Field / Relation）采用 Key + Name 双轨制：
- **Key**: 英文字母 / 数字 / 下划线，长度 2-20，不可以下划线开头，创建后不可修改，用作唯一标识符
- **Name**: 必填的展示名，允许中英文数字下划线，可修改
- 父级引用通过 AppKey（Entity/Relation → App）、EntityKey（RelationEnd → Entity）、RelationKey/TargetFieldKey 等 *Key 字段实现

## 成员清单
client.go:      Make Meta Service 的 HTTP 客户端，提供 Client 类型（含 debug/headers 字段）、Option 函数选项类型、WithDebug/WithHeaders 选项、New(baseURL, token, ...Option) 构造函数、App{Key,Name,Type,Meta,Properties} / Field{Key,Name,Type,Meta,Properties,Validations} / Entity{Key,Name,Type,AppKey,Meta,Properties} / EntityProperties / RelationEnd{EntityKey,Cardinality} / RelationProperties / Relation{Key,Name,Type,AppKey,Meta,Properties} / Schema 类型、CreateApp(key, name, properties) / ListApps(page, size, filter) / DeleteApp(key) / GetApp(key) / CreateEntity(key, name, appKey, fields) / ListEntities(appKey, page, size, filter) / GetEntity(appKey, key) / UpdateEntity(key, name, appKey, fields) / DeleteEntity(key, appKey) / CreateRelation(key, name, appKey, props) / UpdateRelation / ListRelations(appKey, ...) / GetRelation(appKey, key) / DeleteRelation(key, appKey) / GetSchema(appKey) 方法；do() 为底层 POST，支持 debug 输出 curl 命令 + 自定义 headers 注入，post() 处理写操作
client_test.go: 覆盖 CreateApp / DeleteApp / ListApps / WithHeaders / WithDebug 的单元测试（成功/API错误/空列表/格式错误/自定义头/调试模式），用 httptest 隔离网络
record.go:      Data Service 的 Record CRUD 层，提供 DeleteRecordResult / SortField{FieldKey,Order} / ListRecordOpts 类型、CreateRecord(appKey, entityKey, data) / GetRecord(appKey, entityKey, recordID) / UpdateRecord / UpdateRecordsBatch / DeleteRecords / ListRecords 方法；请求体使用 appKey/entityKey/fieldKey 标识符；UpdateRecordsBatch 走 /data/v1/field 端点，其余走 /data/v1/record
record_test.go: 覆盖 CreateRecord / GetRecord / UpdateRecord / UpdateRecordsBatch / DeleteRecords / ListRecords 的单元测试（成功/API错误/可选参数/批量路径验证），用 httptest 隔离网络
integration.go:      Integration 服务调用层，提供 OCROptions 类型 + Client.OCR(filename, reader, opts) 方法；multipart 上传 file/business_id/verify_vat 到 /integration/v1/ocr；6 个 query 参数（coord_restore/specific_pages/crop_complete_image/crop_value_image/merge_digital_elec_invoice/return_ppi）通过 OCROptions.queryString 序列化；data 字段以 map[string]any 透传给上层渲染
integration_test.go: 覆盖 Client.OCR 的单元测试（默认请求/business_id+verify_vat 三态/query 参数序列化/API 错误/传输错误），用 httptest 隔离网络

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
