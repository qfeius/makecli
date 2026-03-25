# makecli - qfeius 的命令行工具
Go + github.com/spf13/cobra

<directory>
cmd/            - Cobra 子命令层（root、version、configure[token/config/set/get]、app、entity、relation、record、apply、diff、update）
internal/api/    - Make Meta/Data Service HTTP 客户端（Client + functional options，X-Make-Target 路由 + 自定义 headers 注入；Meta 操作走 /meta/v1/，Record 操作走 /data/v1/）
internal/build/ - 构建元数据（Version/Date，由 ldflags 注入）
internal/config/ - 凭证与配置管理（读写 ~/.make/credentials 和 ~/.make/config，INI 格式）
internal/update/ - 自更新引擎（GitHub Releases 查询、下载、原子替换二进制）
</directory>

<root>
main.go                        - 程序入口，初始化并调用 cmd.Execute()
</root>

<config>
go.mod                         - 模块声明，module github.com/qfeius/makecli
Makefile                       - 本地构建脚本（build/test/vet/clean），通过 ldflags 注入版本和日期
.goreleaser.yml                - 发布流水线：多平台构建 + 自动推送 Homebrew Tap
.github/workflows/release.yml  - 打 v* tag 时触发 GoReleaser 发布
.github/workflows/ci.yml       - push main / PR 时运行 vet + test
</config>

## 发布流程
```
git tag v1.0.0 && git push --tags
→ GitHub Actions 触发 GoReleaser
→ 构建 linux/darwin/windows × amd64/arm64 二进制
→ 推送 formula 到 qfeius/homebrew-makecli
```

## 安装方式
```bash
brew tap qfeius/makecli
brew install makecli
```
