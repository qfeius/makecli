# makecli - MakeHQ 的命令行工具
Go + github.com/spf13/cobra

<directory>
cmd/            - Cobra 子命令层（root、version、configure）
internal/api/    - Make Meta Service HTTP 客户端（Client/CreateApp，X-Make-Target 路由）
internal/build/ - 构建元数据（Version/Date，由 ldflags 注入）
internal/config/ - 凭证管理（读写 ~/.make/credentials，INI 格式）
</directory>

<config>
go.mod                         - 模块声明，module github.com/MakeHQ/makecli
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
→ 推送 formula 到 MakeHQ/homebrew-makecli
```

## 安装方式
```bash
brew tap MakeHQ/makecli
brew install makecli
```
