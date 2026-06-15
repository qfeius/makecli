# internal/oauth/
> L2 | 父级: /CLAUDE.md

## 成员清单
pkce.go:            PKCE 原语，提供 NewCodeVerifier / NewState（32 字节随机 → base64 raw-url，reader 可注入做确定性测试）/ S256Challenge（sha256 → base64rawurl）
pkce_test.go:       覆盖 NewCodeVerifier（确定性种子 + 短 reader 错误）/ S256Challenge（RFC 7636 Appendix B 向量）
discovery.go:       单跳 OAuth 元数据发现，提供 ServerMetadata 类型与 Discover（GET authorization-server metadata URL → authorization/token/registration 端点；缺 authz/token 端点报错）
discovery_test.go:  覆盖 Discover 成功解析与 500 错误路径，用 httptest 隔离网络
registration.go:    RFC 7591 动态客户端注册，提供 ClientRegistrationRequest/Response 与 RegisterClient（POST registration_endpoint → client_id；缺 client_id 报错）；login 每次注册新 public client
registration_test.go: 覆盖 RegisterClient 请求体断言/成功解析/缺 client_id 错误，用 httptest 隔离网络
flow.go:            登陆流程主原语，提供 Token / AuthorizationRequest / TokenExchangeRequest / CallbackServer 类型与 BuildAuthorizationURL（Resource 空则省略）/ ExchangeAuthorizationCode（authorization_code 换 token，解析 expires_in 为 Expiry）/ StartCallbackServer（绑定 127.0.0.1:0 空闲端口，返回 server + 动态 redirectURL）/ Wait（校验 state 取 code，支持 ctx 超时）/ OpenBrowser（跨平台打开 URL）
flow_test.go:       覆盖 BuildAuthorizationURL query 断言 / ExchangeAuthorizationCode 表单与 Expiry / 回调服务器成功-state不匹配-超时三态，用 httptest + 真实 loopback server

## 设计要点
- 纯协议原语，无 cobra、无持久化、无日志依赖；错误一律经 error 上抛，由 cmd/login.go 编排消费
- 动态端口（StartCallbackServer 绑 127.0.0.1:0）+ 每次注册新 client：两者互锁，消除固定端口与 client_id 持久化两个特殊情况
- 从 deps/contract-cli/internal/oauth 移植，砍掉 bot 的 tenant_access_token、protected-resource 两跳发现、slog.Logger

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
