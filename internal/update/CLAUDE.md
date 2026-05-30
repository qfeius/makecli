# internal/update/
> L2 | 父级: /CLAUDE.md

## 成员清单
update.go:      自更新引擎，CheckLatest 查询 GitHub latest release、ListReleases 拉取最近 N 条 release、GetRelease 按 tag 精确查询、NormalizeTag 规范化版本号、CompareVersions 比较版本（DEV current 视为永远旧）、Apply 下载→**校验 SHA-256**→解压→原子替换；内部 fetchJSON 抽出 GET/解码/状态码共享；内部实现 isNewer（semver 比较，DEV 视为始终可更新）、download/extractBinary 流水线；replaceBinary 定位 exe+校验写权限后委托 installBinary(newBin, exe)（exe 注入便于测试）——copyFile 暂存→备份→安装→清备份，单个 defer 统一清理 stage，安装失败回滚备份，回滚也失败时保留备份并在错误中附带 mv 手动恢复指令；renameFile 包级 seam（=os.Rename）供测试注入失败验证回滚；导出 SetAPIBaseURLForTest 供 cmd 层测试替换 API URL；metaClient（http.Client+10s 超时）约束元数据请求，下载不复用
checksum.go:    供应链完整性闸门，挡在 download 与 replaceBinary 之间；fetchChecksums 拉取 release 的 checksums.txt（短超时 checksumsClient，资产缺失即 fail-closed）、verifyChecksum 纯函数比对归档 SHA-256 与 GoReleaser 格式（"<hex><两空格><文件名>"，大小写不敏感）；任何缺失/不符均返回 error，绝不替换运行中的二进制
update_test.go: 覆盖 isNewer / assetName / findAsset / CheckLatest / ListReleases / NormalizeTag / GetRelease / CompareVersions 的单元测试，用 httptest 隔离网络
checksum_test.go: 覆盖 verifyChecksum（正确/错误/文件名缺失/空内容）、fetchChecksums（存在/资产缺失/非200）、Apply 端到端 fail-closed（缺 checksums 资产 / 文件名缺失 / hash 不符均不替换二进制），用 httptest + t.TempDir + 内存构造 tar.gz
install_test.go: 覆盖 installBinary 的安装/stage 清理/回滚成功/回滚失败恢复提示，通过 renameFile seam 注入失败，exe 用 t.TempDir 隔离

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
