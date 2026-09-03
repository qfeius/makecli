# npm/
> L2 | 父级: /CLAUDE.md

## 成员清单
- `bin/`: 仅含 makecli.js——主包 `@qfeius/makecli` 的 bin 入口，按 process.platform/arch 拼出子包名，require.resolve 子包内 bin/makecli[.exe] 后 spawnSync 透传 argv 与退出码；子包缺失（--omit=optional 或无预编译平台）时给出重装/下载提示并退出 1
- `build.js`: 打包生成器，输入 GoReleaser dist/artifacts.json 的 Binary 条目，goos/goarch → Node os/cpu（windows→win32、amd64→x64），写出 6 个平台子包（package.json 声明 os/cpu，bin/ 携带 0755 二进制）与主包（bin + optionalDependencies 精确钉版 + README/LICENSE），stdout 逐行输出目录、平台子包在前主包最后即发布顺序；buildManifests 为纯函数，parseArgs 拒绝带 v 前缀的版本
- `build.test.js`: node:test 覆盖平台映射、发布顺序、optionalDependencies 钉版、不支持平台报错、parseArgs 校验
- `README.md`: 随主包发布到 npm 的说明（安装、beta 通道、支持平台）

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
