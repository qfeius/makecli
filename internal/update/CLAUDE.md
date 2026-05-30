# internal/update/
> L2 | 父级: /CLAUDE.md

## 成员清单
update.go:      自更新引擎，CheckLatest 查询 GitHub latest release、ListReleases 拉取最近 N 条 release、GetRelease 按 tag 精确查询、NormalizeTag 规范化版本号、CompareVersions 比较版本（DEV current 视为永远旧）、Apply 下载→解压→原子替换；内部 fetchJSON 抽出 GET/解码/状态码共享；内部实现 isNewer（semver 比较，DEV 视为始终可更新）、download/extractBinary/replaceBinary 完整流水线；导出 SetAPIBaseURLForTest 供 cmd 层测试替换 API URL；metaClient（http.Client+10s 超时）用于 JSON 元数据请求（CheckLatest/ListReleases/GetRelease），二进制下载不复用以免大文件被打断
update_test.go: 覆盖 isNewer / assetName / findAsset / CheckLatest / ListReleases / NormalizeTag / GetRelease / CompareVersions 的单元测试，用 httptest 隔离网络

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
