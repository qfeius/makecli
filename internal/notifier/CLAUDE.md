# internal/notifier/
> L2 | 父级: /CLAUDE.md

## 成员清单
- `notice.go`: 对外契约层——Update 类型（同时是 JSON 输出 `_notice.update` 的 wire 形状：current/latest 给程序比较、url 链接、command 给能执行工具的 agent、message 给只读一句话的弱 agent，三种消费者一份数据刻意冗余；newUpdate 构造器统一去 v 前缀并拼 message）+ 进程级 pending（atomic.Pointer 单一真相源，nil=无更新或已被判定链抑制）+ Pending() 读取器（任何 JSON 出口随时读，零阻塞）+ SetPendingForTest 跨包测试钩子
- `cache.go`: 本地缓存层，cacheData(checked_at/latest_version/html_url/channel) 原子读写（temp+rename，避免与并发读撕裂）+ expired 过期判定 + cleanStaleTemps 清扫孤儿临时文件（.update-check-*.json，仅删早于 staleTempAge=1h 的，避免误删并发写入；真实缓存 update-check.json 无该前缀天然豁免）；channel 标记检测结果所属通道，跨通道缓存视为不可用（旧版缓存无此字段 → 空串不匹配 → 刷新一次自愈）；路径 <config.Dir>/update-check.json；读缺失返回零值无错误，损坏返回错误
- `cache_test.go`: 覆盖缓存往返 / 缺失零值 / 过期判定，用 MAKE_CLI_CONFIG_DIR 隔离文件系统
- `decision.go`: 纯判定层，notifierEnabled（三态 env>config>默认开，env 值先 TrimSpace：纯空白视为未设置、非法值下沉）/ pendingUpdate（versionInChannel·CI·skipCommands·跨通道缓存·空缓存·非更新逐条短路，通过则返回 newUpdate，否则 nil；**不含 TTY**——TTY 只是 stderr 渲染的门槛，JSON _notice 不受其约束，两个消费者之间唯一的差异就是 TTY）/ versionInChannel（stable=无预发布段；beta=无预发布段或 betaSegRe `^beta\.[0-9]+$` 白名单——git-describe 伪版本 v0.3.0-16-g… 与模块伪版本天然被拒保持开发态静默，否则 semver「prerelease 低于正式版」会把降级误报成升级；通道守卫必须先于 CompareVersions）/ renderNotice(w, *Update)（提示写 io.Writer：版本行 + 升级命令，不渲染 URL，URL 仅随 JSON _notice.update 给 agent）；skipCommands={version,update,help,completion}
- `decision_test.go`: 穷举 notifierEnabled / versionInChannel 通道矩阵 / pendingUpdate 组合（含跨通道缓存短路、beta 通道提示、wire 字段完整性）+ renderNotice（断言含版本行与升级命令、不含 URL）
- `decision_trim_test.go`: 覆盖 notifierEnabled 对 env 值做 TrimSpace —— 带首尾空白的开关值仍被正确解析
- `notifier.go`: 编排入口，Start(cmdName)（先 pending.Store(nil) 复位；读缓存过判定链 publish 写 pending——零网络立即可用；缓存过期或跨通道才起 goroutine：先 cleanStaleTemps 清扫孤儿 temp，再按通道调 update.CheckLatest(v, beta?) 刷新，成功落盘版本+通道，失败也落盘退避标记 CheckedAt=now+空版本+通道，让慢/离线机器退避 checkInterval 不再每次 spawn，落盘后再 publish 一次重写 pending，recover 兜底 panic）/ Finish()（finishDeadline 收尾 select → Pending()!=nil && isStderrTTY() → renderNotice 到 stderr；不再自己读盘与 LoadSettings，判定全在 Start）/ channelOf（Settings→通道，未知值 fail-safe 回退 stable，报错属 cmd resolveChannel 职责）；isStderrTTY 包级闭包便于测试替换
- `notifier_test.go`: Start 刷新落盘 / 新鲜缓存跳过 / beta 通道走 /releases 列表端点并落盘通道 / 新鲜但跨通道缓存触发刷新 / pending 发布（新鲜缓存零网络即时、过期缓存刷新后重写、开关关闭与 skipCommands 保持 nil）/ Finish 禁用不阻塞 / Finish 仅 TTY 渲染（同一份 pending 非 TTY 静默），用 httptest + SetAPIBaseURLForTest 隔离网络，<-done 确定性同步无 sleep，captureStderr 管道劫持 stderr
- `backoff_test.go`: 覆盖刷新失败退避落盘（CheckedAt 前进、版本留空、判为新鲜）/ cleanStaleTemps 删旧留新不碰真实缓存 / Start 过期刷新时清扫孤儿 temp，用 httptest + SetAPIBaseURLForTest 隔离网络

## 关键常量
checkInterval=24h · finishDeadline=250ms · staleTempAge=1h · envEnable=MAKE_CLI_UPDATE_NOTIFIER · updateCommand="makecli update"

## 接入点
被 cmd.Execute 在命令头尾钩入：Start(commandName) 在 Execute 前写 pending 并按需后台刷新，Finish() 仅在命令成功（err == nil）且错误已报告之后调用，收尾 stderr 提示（仅 TTY；对齐 gh：失败不提示，提示永远在所有输出之后）；cmd/output.go writeJSON 读 Pending() 在 `--output json` 顶层对象末尾追加 `_notice.update`（不问 TTY，agent 通道）。复用 internal/update.CheckLatest、internal/config.{Dir,LoadSettings}、internal/build.Version

## 与 lark-cli 的对应
lark-cli 同款三段式：启动零网络读缓存 → 进程级 atomic pending → JSON 信封出口统一挂 `_notice`。差异：makecli 没有错误 JSON 信封（错误仍是 stderr 文本），故 _notice 只随成功的 JSON 输出；lark-cli 无 stderr 文本版，makecli 保留给 TTY 上的人看

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
