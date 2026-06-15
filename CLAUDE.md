# makecli - qfeius 的命令行工具
Go + github.com/spf13/cobra

<directory>
agents/          - app init 模板文件（CLAUDE.md / AGENTS.md），通过 embed.FS 编译进二进制
cmd/            - Cobra 子命令层（root、version、configure[token/config/set/get/verify]、app[create/list/init/delete/deploy]、entity、relation、record、apply、diff、update、schema、integration[ocr]、preflight、login）
internal/api/    - Make Meta/Data/Integration Service HTTP 客户端（Client + functional options，X-Make-Target 路由 + 自定义 headers 注入；Meta 操作走 /meta/v1/，Record 操作走 /data/v1/，Integration 操作走 /integration/v1/，代码仓库操作走独立 host 的 /code/v1/repository）
internal/oauth/ - 浏览器 OAuth 登陆原语（PKCE + 单跳 discovery + RFC 7591 动态注册 + 授权URL/换token + 动态端口回调 server），从 contract-cli 移植，被 cmd/login 编排
internal/build/ - 构建元数据（Version/Date，由 ldflags 注入）
internal/config/ - 凭证与配置管理（读写 credentials 和 config，INI 格式；默认 ~/.make，可用 $MAKE_CLI_CONFIG_DIR 覆盖）
internal/update/ - 自更新引擎（GitHub Releases 查询、下载、原子替换二进制）
internal/notifier/ - 自动更新提示（读本地缓存零延迟判定，过期后台 goroutine 刷新，stderr+仅TTY 提示；三态开关 env MAKE_CLI_UPDATE_NOTIFIER > config [settings] > 默认开）
</directory>

<root>
main.go                        - 程序入口，初始化并调用 cmd.Execute()
</root>

<config>
go.mod                         - 模块声明，module github.com/qfeius/makecli
Makefile                       - 本地构建脚本（build/test/vet/clean），通过 ldflags 注入版本和日期
.goreleaser.yml                - 发布流水线：多平台构建 + 自动推送 Homebrew Tap
.github/workflows/release.yml  - 打 v* tag 时触发 GoReleaser 发布
.github/workflows/ci.yml       - push main / PR 时运行 golangci-lint + vet + test（PR 另跑 Claude 安全扫描）
</config>

## 发布流程
```
git tag v1.0.0 && git push --tags
→ GitHub Actions 触发 GoReleaser
→ 构建 linux/darwin/windows × amd64/arm64 二进制
→ 推送 formula 到 qfeius/homebrew-makecli
```

## 常用命令
```bash
make build          # 构建到 bin/makecli（自动注入版本和日期）
make test           # 运行全部测试
make vet            # 静态检查
make local          # 构建并安装到 ~/.local/bin
```

## 开发纪律（AI Agent 必读）
验证必须**门控**提交——血泪教训，曾三次推送编译失败的代码到 main。

1. **Edit 失败必核实**：返回 "String to replace not found" = 该编辑**未生效**，必须重读文件重做，绝不假设成功后继续。一次静默失败的 import/调用点替换就会让整包 build red。
2. **改 Go 代码后先验证再提交**：单独跑 `make vet && make test`（+ `golangci-lint run ./...`），**确认 exit 0** 才进入 `git commit` / `git push`。**禁止**在同一批工具调用里 test + commit + push——red 会来不及拦住提交。
3. **gofmt 不查编译**：`gofmt -l` 干净 ≠ 能编译。CI 门禁含 golangci-lint，本地须 0 issues 才 push（新测试常踩 gocritic：`stringXbytes` 改 `bytes.Equal`、手写线性查找改 `slices.Contains`）。
4. **沙箱假性失败**：`go vet/test/build` 在命令沙箱下因 module cache（`~/code/go/pkg/mod`）不可写而报 `operation not permitted`，非代码错误；Go 工具链命令直接禁用沙箱跑。
5. **跨 worktree 落地用 patch/逐段 Edit**，勿整文件 cp 覆盖——worktree 可能基于陈旧 commit，会静默回退他人近期工作。

## 安装方式
```bash
brew tap qfeius/makecli
brew install makecli
```
