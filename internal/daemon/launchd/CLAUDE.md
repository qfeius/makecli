# internal/daemon/launchd/
> L2 | 父级: ../CLAUDE.md

daemon 的 macOS 托管面:把前台 `makecli daemon` 固化成用户级 LaunchAgent(~/Library/LaunchAgents/cn.qfei.makecli.daemon.plist),由 launchd 负责登录自启与退出拉起(RunAtLoad + KeepAlive)。只做 macOS,平台守卫在 cmd 层。

两条硬约束决定了本包的形状:
1. **launchd 不继承 shell 环境**——没有 .zshrc、没有 export 的变量。故 Config 里每一项都必须是「已解析的最终值」:gateway 地址等参数写死进 ProgramArguments,PATH 显式进 EnvironmentVariables(缺了就探测不到 claude / codex)。
2. **plist 留在磁盘上就等于开机自启**——launchd 每次登录扫描 LaunchAgents 目录。故 stop 语义是 bootout + 删文件,只卸载不删会"停了却自己回来"。

成员清单
launchd.go: Label 常量、Config(BinaryPath/Args/Env/WorkingDir/LogPath)、Status、ErrNotInstalled 哨兵;Render(text/template + xml.EscapeText 逐值转义,环境变量按键名排序保证同 Config 同字节)、PlistPath、Install(写 plist → bootout 旧实例 → bootstrap → kickstart,覆盖式:改了参数重跑立刻生效;bootstrap 带 3 次退避重试兜 launchd 异步卸载竞态——bootout 立刻返回但服务真退出是异步的,紧跟的 bootstrap 会报 `5: Input/output error`)、Uninstall(bootout + 删 plist,返回此前是否托管)、Restart(复用磁盘 plist 不重渲染,故 start 敲定的参数原样保留;plist 不存在返回 ErrNotInstalled)、Query(plist 给"配置成什么样",launchctl list 给"现在活没活");launchctl 调用收口在包级 runLaunchctl 单一出口(单测打桩点),域固定 gui/$UID;parsePlist 按 <key> 与其后兄弟节点配对读回自家 plist(嵌套 dict 整体跳过,不是通用解析器)
launchd_test.go: 渲染转义与读回往返、渲染确定性、Install/Uninstall/Restart 的 launchctl 调用序与文件效果、bootstrap 重试自愈与失败不 kickstart、Query 三态(运行中/已托管未运行/未托管);打桩 runLaunchctl + t.Setenv("HOME") 隔离,非 macOS 机器也能跑全路径

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
