# internal/config/
> L2 | 父级: /CLAUDE.md

## 成员清单
atomic.go:           落盘原语 atomicWrite（同目录 temp + rename 原子替换，render 出错清理不留残留），被 Save/SaveConfig 复用，消除 O_TRUNC 直写的崩溃损坏窗口
atomic_test.go:      覆盖 atomicWrite 落盘内容/权限0600/无临时残留/覆盖既有文件/render 出错传播不落盘
paths.go:            配置目录解析中枢，提供 Dir 函数与 EnvConfigDir 常量，$MAKE_CLI_CONFIG_DIR 非空时覆盖默认 ~/.make
paths_test.go:       覆盖 Dir 默认回退与 $MAKE_CLI_CONFIG_DIR 覆盖语义，串联 Save/Load 的 env 隔离测试
credentials.go:      读写 credentials 文件（默认 ~/.make/credentials，INI 格式），提供 Load/Save/CredentialsPath，Credentials/Profile 类型；Save 经 atomicWrite 原子落盘，并经 ValidateProfileName 拒绝保留名 settings 作 profile
credentials_test.go: 覆盖 parseINI（白盒）+ Load/Save 全路径测试，用 t.Setenv("HOME",...) 隔离文件系统
config.go:           读写 config 文件（默认 ~/.make/config，INI 格式），提供 LoadConfig/SaveConfig/SetSetting/ConfigPath，Config/ConfigProfile 类型（含 ServerURL/RepoServerURL/AuthServerURL/XTenantID/OperatorID，INI key: server-url / repo-server-url / auth-server-url / X-Tenant-ID / X-Operator-ID；auth-server-url 为 OAuth 身份服务器基址，供 login 派生 .well-known 元数据地址）；唯一写路径 saveConfigWithSettings（profile 段 + 显式 [settings] 段，经 ValidateProfileName 拒绝保留名 settings 作 profile）：SaveConfig 传磁盘现状以保留 [settings]，SetSetting 读-改-写单个全局键（让 [settings] 可写）；parseINISections 通用 INI 解析器供 settings.go 复用
config_test.go:      覆盖 parseConfigINI（白盒）+ LoadConfig/SaveConfig 全路径测试，复用 writeTempINI helper
environment.go:      后端环境拓扑中枢，把 dev/test/production 三套 URL（make/make-repo/myaccount，遵循 {dev-,test-,""} 前缀规律）收成一等 Environment preset；提供 LookupEnvironment（空名回退 DefaultEnvironment=dev，未知名 ok=false）/ EnvironmentNames；作 cmd 层 URL 解析链的兜底层（flag > profile config > 环境 preset）
environment_test.go: 覆盖 LookupEnvironment（空回退/三环境完整 preset/未知名）+ EnvironmentNames 排序与 DefaultEnvironment
settings.go:         读取 config 文件 [settings] 全局段，提供 Settings 类型（CheckForUpdates *bool / Environment string）与 LoadSettings、ValidateProfileName（拒绝保留段名 settings 作 profile，防 [settings] profile 段与全局段碰撞）；settingsSection 常量由此定义，profile 解析层自动跳过该段；写入走 config.go 的 SetSetting
settings_test.go:    覆盖 LoadSettings（check-for-updates 三态 + environment 读取）+ SetSetting 写入并保留 profile/其他 settings 键 + LoadConfig 隔离 [settings] 段 + ValidateProfileName/Save 拒绝保留名

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
