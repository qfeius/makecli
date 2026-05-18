# internal/update/
> L2 | 父级: /CLAUDE.md

## 成员清单
update.go:      自更新引擎，CheckLatest 查询 GitHub latest release、ListReleases 拉取最近 N 条 release、Apply 下载→解压→原子替换；内部实现 isNewer（semver 比较，DEV 视为始终可更新）、download/extractBinary/replaceBinary 完整流水线；导出 SetAPIBaseURLForTest 供 cmd 层测试替换 API URL
update_test.go: 覆盖 isNewer / assetName / findAsset / CheckLatest / ListReleases 的单元测试，用 httptest 隔离网络

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
