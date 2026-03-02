# internal/api/
> L2 | 父级: /CLAUDE.md

## 成员清单
client.go:      Make Meta Service 的 HTTP 客户端，提供 Client 类型、New 构造函数、App 类型、CreateApp / ListApps 方法；do() 为底层 POST，post() 处理写操作，ListApps 处理列表响应
client_test.go: 覆盖 CreateApp / ListApps 的单元测试（成功/API错误/空列表/格式错误），用 httptest 隔离网络

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
