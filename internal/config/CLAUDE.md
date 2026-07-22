# internal/config/
> L2 | 父级: /CLAUDE.md

## 成员清单
- `atomic.go`: 落盘原语 atomicWrite（同目录 temp + rename 原子替换，render 出错清理不留残留），被 Save/SaveConfig 复用，消除 O_TRUNC 直写的崩溃损坏窗口
- `atomic_test.go`: 覆盖 atomicWrite 落盘内容/权限0600/无临时残留/覆盖既有文件/render 出错传播不落盘
- `paths.go`: 配置目录解析中枢，提供 Dir 函数与 EnvConfigDir 常量，$MAKE_CLI_CONFIG_DIR 非空时覆盖默认 ~/.make
- `paths_test.go`: 覆盖 Dir 默认回退与 $MAKE_CLI_CONFIG_DIR 覆盖语义，串联 Save/Load 的 env 隔离测试
- `credentials.go`: 读写 credentials 文件（默认 ~/.make/credentials，INI 格式），提供 Load/Save/CredentialsPath，Credentials/Profile 类型；Save 经 atomicWrite 原子落盘，落盘前过 INI 注入防线（ValidateProfileName 文法+保留名、validateINIValue 拒 token 含换行/首尾空白）
- `credentials_test.go`: 覆盖 parseINI（白盒）+ Load/Save 全路径测试 + INI 注入拒绝（"evil]\n[other" profile 名、token 带换行/首尾空白，且拒绝时不落文件），用 t.Setenv("HOME",...) 隔离文件系统
- `config.go`: 读写 config 文件（默认 ~/.make/config，INI 格式），提供 LoadConfig/SaveConfig/SetSetting/ConfigPath，Config/ConfigProfile 类型（含 MetaServerURL/RepoServerURL/AuthServerURL/XTenantID/OperatorID，INI key: meta-server-url / repo-server-url / auth-server-url / X-Tenant-ID / X-Operator-ID；auth-server-url 为 OAuth 身份服务器基址，供 login 派生 .well-known 元数据地址）；唯一写路径 saveConfigWithSettings（profile 段 + 显式 [settings] 段，经 ValidateProfileName 拒绝保留名 settings 作 profile）：SaveConfig 传磁盘现状以保留 [settings]，SetSetting 读-改-写单个全局键（让 [settings] 可写）；落盘前过 INI 注入防线（ValidateProfileName + validateINIKey/validateINIValue：键限保守文法、值拒换行与首尾空白，防 "x\n[evil]" 伪造 section）；validateINIKey/validateINIValue 由此定义、credentials.go 复用；parseINISections 通用 INI 解析器供 settings.go 复用
- `config_test.go`: 覆盖 parseConfigINI（白盒）+ LoadConfig/SaveConfig 全路径测试 + INI 注入拒绝（profile 值带换行/首尾空白、SetSetting 非法键与换行值），复用 writeTempINI helper
- `channel.go`: 发布通道域常量（ChannelStable/ChannelBeta/DefaultChannel=stable + ChannelNames），与 environment.go 同责的域取值单一真相源；stable 只跟踪正式版、beta 额外跟踪 prerelease，被 cmd 层（resolveChannel/setChannel）与 internal/notifier（channelOf/versionInChannel）消费
- `channel_test.go`: 覆盖通道常量/名称集与 LoadSettings 读取 [settings] channel（配置/未配置三态），$MAKE_CLI_CONFIG_DIR 隔离文件系统
- `environment.go`: 后端环境拓扑中枢，把 dev/test/production 三套**主机基址**（make/make-repo/myaccount，均为 scheme://host 不含路径；dev/test 用 qtech.cn 且带 {dev-,test-} 前缀，production 用 qfei.cn）收成一等 Environment preset；Meta/Repo 的网关前缀 /api/make 不在此、由 cmd 层 withGateway 补齐，Auth 基址供 login 追加 .well-known；提供 LookupEnvironment（空名回退 DefaultEnvironment=production，未知名 ok=false）/ EnvironmentNames；作 cmd 层 URL 解析链的兜底层（flag > profile config > 环境 preset）
- `environment_test.go`: 覆盖 LookupEnvironment（空回退/三环境完整 preset/未知名）+ EnvironmentNames 排序与 DefaultEnvironment
- `settings.go`: 读取 config 文件 [settings] 全局段，提供 Settings 类型（CheckForUpdates *bool / Environment string / Channel string）与 LoadSettings、ValidateProfileName（保守文法 ^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$ 防 profile 名注入 INI section 语法 + 拒绝保留段名 settings 作 profile，防 [settings] profile 段与全局段碰撞；validProfileName 正则由此定义）；settingsSection 常量由此定义，profile 解析层自动跳过该段；写入走 config.go 的 SetSetting
- `settings_test.go`: 覆盖 LoadSettings（check-for-updates 三态 + environment 读取）+ SetSetting 写入并保留 profile/其他 settings 键 + LoadConfig 隔离 [settings] 段 + ValidateProfileName 文法全谱（合法名/64 字符边界/空名/括号换行空白/非法首字符/超长全拒）/Save 拒绝保留名

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
