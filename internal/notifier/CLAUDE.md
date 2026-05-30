# internal/notifier/
> L2 | 父级: /CLAUDE.md

## 成员清单
cache.go:        本地缓存层，cacheData(checked_at/latest_version/html_url) 原子读写（temp+rename，避免与并发读撕裂）+ expired 过期判定；路径 <config.Dir>/update-check.json；读缺失返回零值无错误，损坏返回错误
cache_test.go:   覆盖缓存往返 / 缺失零值 / 过期判定，用 MAKE_CLI_CONFIG_DIR 隔离文件系统
decision.go:     纯判定层，notifierEnabled（三态 env>config>默认开）/ shouldNotify（isReleaseVersion·CI·非TTY·skipCommands·空缓存逐条短路，DEV-guard 必须先于 CompareVersions）/ isReleaseVersion（拒绝 DEV/非法/任何带 prerelease 的版本——git-describe 伪版本 v0.3.0-16-g… 视为开发态，否则 semver「prerelease 低于正式版」会把降级误报成升级）/ renderNotice（提示写 io.Writer）；skipCommands={version,update,help,completion}
decision_test.go: 穷举 notifierEnabled / shouldNotify 组合 + renderNotice 有/无 URL 两分支
notifier.go:     编排入口，Start（缓存过期才起 goroutine 调 update.CheckLatest 刷新，recover 兜底 panic）/ Finish（finishDeadline 收尾 select→LoadSettings→判定链→renderNotice 到 stderr）；isStderrTTY 包级闭包便于测试替换
notifier_test.go: Start 刷新落盘 / 新鲜缓存跳过 / Finish 禁用不阻塞，用 httptest + SetAPIBaseURLForTest 隔离网络，<-done 确定性同步无 sleep

## 关键常量
checkInterval=24h · finishDeadline=250ms · envEnable=MAKE_CLI_UPDATE_NOTIFIER

## 接入点
被 cmd.Execute 在命令头尾钩入：Start() 并行刷新缓存，Finish(commandName) 收尾提示；复用 internal/update.CheckLatest、internal/config.{Dir,LoadSettings}、internal/build.Version

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
