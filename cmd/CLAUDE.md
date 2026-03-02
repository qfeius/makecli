# cmd/
> L2 | 父级: /CLAUDE.md

## 成员清单
root.go:             根命令入口，挂载所有子命令，对外暴露 Execute(version, date)
version.go:          version 子命令，格式化版本输出（参考 GitHub CLI 模式）
version_test.go:     覆盖 formatVersion / changelogURL 的纯函数测试
configure.go:        configure 子命令，交互式写入 ~/.make/credentials，支持 --profile
configure_test.go:   覆盖 mask / validateJWT 的纯函数测试
app.go:              app 命令组，挂载 app 相关子命令
app_create.go:       app create 子命令，调用 Meta Server API（MakeService.CreateResource）创建 App；支持 --profile 和 --server flags
app_create_test.go:  覆盖 runAppCreate 的单元测试（成功/无凭证/API错误/未知profile），用 httptest 隔离网络
app_list.go:         app list 子命令，调用 MakeService.ListResources 列出 org 下全部 App，tabwriter 对齐输出；支持 --profile / --server / --size flags
app_list_test.go:    覆盖 runAppList 的单元测试（成功/空列表/无凭证/API错误），用 httptest 隔离网络
app_init.go:         app init 子命令，在已有 Folder 内创建 provider 对应配置文件（anthropic→CLAUDE.md / openai→AGENTS.md / google→GEMINI.md / cursor→.cursorrules）
app_init_test.go:    覆盖 runAppInit 的文件系统测试（含全 provider 覆盖）
app_delete.go:       app delete 子命令，调用 Meta Server API（MakeService.DeleteResource）删除指定 App；支持 --profile 和 --server flags
app_delete_test.go:  覆盖 runAppDelete 的单元测试（成功/无凭证/API错误/未知profile），用 httptest 隔离网络

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
