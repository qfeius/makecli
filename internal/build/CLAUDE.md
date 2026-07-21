# internal/build/
> L2 | 父级: /CLAUDE.md

## 成员清单
- `build.go`: Version/Date 变量持有者，默认 "DEV"/"", 构建时通过 ldflags 覆盖；init() 从 debug.ReadBuildInfo 兜底——Version 取 module 版本，Date 经 deriveDate 从 vcs.time 截取 YYYY-MM-DD
- `build_test.go`: 覆盖 deriveDate（vcs.time 截取/无 vcs.time/空 settings/异常短值不越界）

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
