# internal/config/
> L2 | 父级: /CLAUDE.md

## 成员清单
atomic.go:           落盘原语 atomicWrite（同目录 temp + rename 原子替换，render 出错清理不留残留），被 Save/SaveConfig 复用，消除 O_TRUNC 直写的崩溃损坏窗口
atomic_test.go:      覆盖 atomicWrite 落盘内容/权限0600/无临时残留/覆盖既有文件/render 出错传播不落盘
paths.go:            配置目录解析中枢，提供 Dir 函数与 EnvConfigDir 常量，$MAKE_CLI_CONFIG_DIR 非空时覆盖默认 ~/.make
paths_test.go:       覆盖 Dir 默认回退与 $MAKE_CLI_CONFIG_DIR 覆盖语义，串联 Save/Load 的 env 隔离测试
credentials.go:      读写 credentials 文件（默认 ~/.make/credentials，INI 格式），提供 Load/Save/CredentialsPath，Credentials/Profile 类型；Save 经 atomicWrite 原子落盘
credentials_test.go: 覆盖 parseINI（白盒）+ Load/Save 全路径测试，用 t.Setenv("HOME",...) 隔离文件系统
config.go:           读写 config 文件（默认 ~/.make/config，INI 格式），提供 LoadConfig/SaveConfig/ConfigPath，Config/ConfigProfile 类型（含 RepoServerURL/XTenantID/OperatorID，INI key: server-url / repo-server-url / X-Tenant-ID / X-Operator-ID）；SaveConfig 经 atomicWrite 原子落盘并保留 [settings] 段；parseINISections 通用 INI 解析器供 settings.go 复用
config_test.go:      覆盖 parseConfigINI（白盒）+ LoadConfig/SaveConfig 全路径测试，复用 writeTempINI helper
settings.go:         读取 config 文件 [settings] 全局段，提供 Settings 类型与 LoadSettings；settingsSection 常量由此定义，profile 解析层自动跳过该段
settings_test.go:    覆盖 LoadSettings 三态（无文件/false/true）+ LoadConfig 隔离 [settings] 段的单元测试

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
