# internal/config/
> L2 | 父级: /CLAUDE.md

## 成员清单
paths.go:            配置目录解析中枢，提供 Dir 函数与 EnvConfigDir 常量，$MAKE_CLI_CONFIG_DIR 非空时覆盖默认 ~/.make
paths_test.go:       覆盖 Dir 默认回退与 $MAKE_CLI_CONFIG_DIR 覆盖语义，串联 Save/Load 的 env 隔离测试
credentials.go:      读写 credentials 文件（默认 ~/.make/credentials，INI 格式），提供 Load/Save/CredentialsPath，Credentials/Profile 类型
credentials_test.go: 覆盖 parseINI（白盒）+ Load/Save 全路径测试，用 t.Setenv("HOME",...) 隔离文件系统
config.go:           读写 config 文件（默认 ~/.make/config，INI 格式），提供 LoadConfig/SaveConfig/ConfigPath，Config/ConfigProfile 类型（含 XTenantID/OperatorID）
config_test.go:      覆盖 parseConfigINI（白盒）+ LoadConfig/SaveConfig 全路径测试，复用 writeTempINI helper

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
