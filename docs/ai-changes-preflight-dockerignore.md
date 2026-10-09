# AI 变更记录：preflight 检查 .dockerignore

## 2026-10-09（规约优化）

- 变更摘要：修复未命中或被前序规则遮蔽的非法规则漏检，优化 D2 说明与修复提示。
- 涉及文件/模块：`cmd/preflight.go`、`cmd/preflight_test.go`、根与 `cmd/CLAUDE.md`、`docs/CLAUDE.md`。
- 关键逻辑：纯解析函数逐条触发规则编译；验证反向规则时临时先匹配 `**`，实际判断仍保留原规则顺序。
- 决策：D2 只检查构建模板的已知入口及配置路径；文档明确不保证完整 `dist` 依赖，语法/读取错误单独给出修复提示。
- 验证：新增非法规则用例先失败后通过，preflight 专项及 vet 通过；全量 test 仍受既有更新用例配置影响，lint 仍报未改动测试文件的 6 条静态检查信息。

## 2026-10-09

- 变更摘要：组件模式预检新增 D2，发现 `.dockerignore` 排除镜像打包必需文件时阻断部署并给出修复路径。
- 涉及文件/模块：`cmd/preflight.go`、`cmd/preflight_test.go`、`cmd/CLAUDE.md`、`CLAUDE.md`、`go.mod`、`go.sum`。
- 关键逻辑：使用 Docker 的 ignorefile/patternmatcher 语义，检查 UI/Service 镜像的关键 COPY 输入（含可选 `nginx.conf`），支持通配符与反向规则；无需预先生成 `dist`。
- 决策：保留 `.git`、`node_modules` 等正常排除，仅提示用户调整误排除的构建输入。
- 验证：新用例先红后绿；`make vet`、其余 Go 测试、Node 测试及兼容 Go 1.25 的 golangci-lint 通过；本机 beta 更新通道使一项既有测试需隔离配置运行。
