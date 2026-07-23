# makecli - qfeius 的命令行工具
Go 1.25.8 + github.com/spf13/cobra + github.com/go-git/go-git/v5（app init/create/deploy 纯 Go 操作 git，不 shell-out，二进制自包含；deploy 是「纯 push 已提交状态」，提交时机交还用户）+ charm.land/huh/v2（app delete 删除确认交互表单；go directive 因其要求顶到 1.25.8，需 ≥1.25.8 工具链构建）

<directory>

- `agents/` - 脚手架模板文件（CLAUDE.md.tmpl / AGENTS.md.tmpl 由 app create 写出；gitignore.tmpl 是 .gitignore 期望清单单一真相源，由 cmd/git ensureGitignore 增量补齐），.tmpl 后缀避开 GEB L2 撞名，通过 embed.FS 编译进二进制
- `cmd/` - Cobra 子命令层（root、version、configure[token/config/set/get/verify]、app[create/init/list/delete/deploy]、entity、relation、record、apply、diff、update、skills[list/update/remove]、schema、integration[ocr]、preflight、login、daemon[隐藏]、agent[隐藏]）；git.go 收口共享 go-git 原语
- `internal/api/` - Make Meta/Data/Integration Service HTTP 客户端（Client + functional options，X-Make-Target 路由 + 自定义 headers 注入 + 每请求注入 W3C Traceparent/X-Log-Id + WithDryRun 时注入 X-Dry-Run（CreateResource 全族写命令 --dry-run 共用：远端跑真实流程但 ROLLBACK 不落库）；Meta 操作走 /meta/v1/，Record 操作走 /data/v1/，Integration 操作走 /integration/v1/，代码仓库操作走独立 host 的 /code/v1/repository）
- `internal/trace/` - W3C Trace Context 出站头生成（零依赖手写 traceparent v00；进程级 trace-id 单一真相源——每次 CLI 调用一个、全程稳定，X-Log-Id=trace-id 段，parent-id 每请求新生成），被 internal/api 请求咽喉点消费
- `internal/oauth/` - 浏览器 OAuth 登陆原语（PKCE + 单跳 discovery + RFC 7591 动态注册 + 授权URL/换token + 动态端口回调 server），从 contract-cli 移植，被 cmd/login 编排
- `internal/build/` - 构建元数据（Version/Date，由 ldflags 注入）
- `internal/config/` - 凭证与配置管理（读写 credentials 和 config，INI 格式；默认 ~/.make，可用 $MAKE_CLI_CONFIG_DIR 覆盖）；内建 dev/test/production 环境 preset（后端主机基址三件套，scheme://host 不含路径），全局 [settings] environment 选当前环境，URL 解析链 flag > profile config > 环境 preset；发布通道常量 stable/beta（channel.go），全局 [settings] channel 选通道；Meta/Repo 网关前缀 /api/make 由 cmd 层 withGateway 自动补齐（配置只写主机名）
- `internal/update/` - 自更新引擎（GitHub Releases 查询、下载、原子替换二进制）；CheckLatest 双通道：stable 走 /releases/latest（GitHub 服务端过滤 prerelease），beta 走 /releases 列表取 semver 最高（候选含稳定版，反超自动收敛回 stable）
- `internal/skillsync/` - Make platform skills 同步/清单/删除（Sync 默认每次 npx 安装/升级 qfeius/make-platform-skills --all，--skip-skills 跳过；List 合并 lockfile + SKILL.md + GitHub Contents API 做 outdated 比对；Remove 来源校验后透传 npx skills remove），被 cmd/update 与 cmd/skills 消费
- `internal/daemon/` - Agent 平台设备接入（隐藏命令 `makecli daemon`）：注册/心跳/claim 轮询驱动本机 coding CLI（claude-code / codex adapter），最终答复经 @Name 解析产出结构化 mention 块（互@触发，agent-design/Design.md §7.5），协议 wire 类型镜像 agent-design/Contract.md（公开仓库无法 import 私有 agent-contract）
- `internal/agent/` - keyless 本地 agent（隐藏命令 `makecli agent`，agent-design/Design.md §8.2）：OpenAI 兼容 SSE 客户端指向 gateway /v1/chat/completions（平台 token 只开模型门，设备端零厂商 key）+ 会话编排（一次性 -p / 交互 REPL，历史进程内存续）；v1 纯聊天，loop/tools/context/session 四模块随后续 goal 进入
- `internal/notifier/` - 自动更新提示（读本地缓存零延迟判定，过期或跨通道后台 goroutine 刷新，stderr+仅TTY 提示；三态开关 env MAKE_CLI_UPDATE_NOTIFIER > config [settings] > 默认开；按 [settings] channel 检查与提示，缓存带 channel 字段跨通道失效，beta.N 白名单拒 git-describe 伪版本）

</directory>

<root>

- `main.go` - 程序入口，初始化并调用 cmd.Execute()

</root>

<config>

- `go.mod` - 模块声明，module github.com/qfeius/makecli
- `Makefile` - 本地构建脚本（build/test/vet/clean），通过 ldflags 注入版本和日期
- `CHANGELOG.md` - 版本变更记录（Keep a Changelog 格式）；`update --check` 链接指向此文件；发版时由 /ship Step 5 从 git log 重生成并提交回 main
- `.goreleaser.yml` - 发布流水线：多平台构建 + 自动推送 Homebrew Tap
- `.github/workflows/release.yml` - 打 v* tag 时触发 GoReleaser 发布
- `.github/workflows/ci.yml` - push main / PR 时运行 golangci-lint + vet + test（PR 另跑 Claude 安全扫描）

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
