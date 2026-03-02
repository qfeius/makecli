# internal/api/
> L2 | 父级: /CLAUDE.md

## 成员清单
client.go:      Make Meta Service 的 HTTP 客户端，提供 Client 类型、New 构造函数、CreateApp 方法；所有请求均 POST，通过 X-Make-Target header 分发操作
client_test.go: 覆盖 CreateApp 的单元测试（成功/API错误/格式错误），用 httptest 隔离网络

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
