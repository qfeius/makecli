# internal/notifier/
> L2 | 父级: /CLAUDE.md

## 成员清单
- `cache.go`: 本地缓存层，cacheData(checked_at/latest_version/html_url/channel) 原子读写（temp+rename，避免与并发读撕裂）+ expired 过期判定 + cleanStaleTemps 清扫孤儿临时文件（.update-check-*.json，仅删早于 staleTempAge=1h 的，避免误删并发写入；真实缓存 update-check.json 无该前缀天然豁免）；channel 标记检测结果所属通道，跨通道缓存视为不可用（旧版缓存无此字段 → 空串不匹配 → 刷新一次自愈）；路径 <config.Dir>/update-check.json；读缺失返回零值无错误，损坏返回错误
- `cache_test.go`: 覆盖缓存往返 / 缺失零值 / 过期判定，用 MAKE_CLI_CONFIG_DIR 隔离文件系统
- `decision.go`: 纯判定层，notifierEnabled（三态 env>config>默认开，env 值先 TrimSpace：纯空白视为未设置、非法值下沉）/ shouldNotify（versionInChannel·CI·非TTY·skipCommands·跨通道缓存·空缓存逐条短路，通道守卫必须先于 CompareVersions）/ versionInChannel（stable=无预发布段；beta=无预发布段或 betaSegRe `^beta\.[0-9]+$` 白名单——git-describe 伪版本 v0.3.0-16-g… 与模块伪版本天然被拒保持开发态静默，否则 semver「prerelease 低于正式版」会把降级误报成升级）/ renderNotice（提示写 io.Writer）；skipCommands={version,update,help,completion}
- `decision_test.go`: 穷举 notifierEnabled / versionInChannel 通道矩阵 / shouldNotify 组合（含跨通道缓存短路、beta 通道提示）+ renderNotice 有/无 URL 两分支
- `decision_trim_test.go`: 覆盖 notifierEnabled 对 env 值做 TrimSpace —— 带首尾空白的开关值仍被正确解析
- `notifier.go`: 编排入口，Start（缓存过期或跨通道才起 goroutine：先 cleanStaleTemps 清扫孤儿 temp，再按通道调 update.CheckLatest(v, beta?) 刷新；成功落盘版本+通道，失败也落盘退避标记 CheckedAt=now+空版本+通道，让慢/离线机器退避 checkInterval 不再每次 spawn，recover 兜底 panic）/ Finish（finishDeadline 收尾 select→LoadSettings→判定链（含通道）→renderNotice 到 stderr）/ channelOf（Settings→通道，未知值 fail-safe 回退 stable，报错属 cmd resolveChannel 职责）；isStderrTTY 包级闭包便于测试替换
- `notifier_test.go`: Start 刷新落盘 / 新鲜缓存跳过 / beta 通道走 /releases 列表端点并落盘通道 / 新鲜但跨通道缓存触发刷新 / Finish 禁用不阻塞，用 httptest + SetAPIBaseURLForTest 隔离网络，<-done 确定性同步无 sleep
- `backoff_test.go`: 覆盖刷新失败退避落盘（CheckedAt 前进、版本留空、判为新鲜）/ cleanStaleTemps 删旧留新不碰真实缓存 / Start 过期刷新时清扫孤儿 temp，用 httptest + SetAPIBaseURLForTest 隔离网络

## 关键常量
checkInterval=24h · finishDeadline=250ms · staleTempAge=1h · envEnable=MAKE_CLI_UPDATE_NOTIFIER

## 接入点
被 cmd.Execute 在命令头尾钩入：Start() 并行刷新缓存，Finish(commandName) 收尾提示；复用 internal/update.CheckLatest、internal/config.{Dir,LoadSettings}、internal/build.Version

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
