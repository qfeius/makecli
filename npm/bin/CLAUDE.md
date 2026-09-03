# npm/bin/
> L2 | 父级: /npm/CLAUDE.md

## 成员清单
- `makecli.js`: 主包 `@qfeius/makecli` 的 bin 入口（`bin` 字段指向此文件），按 process.platform/arch 拼出 `@qfeius/makecli-<platform>-<arch>`，require.resolve 子包内 bin/makecli[.exe] 后 spawnSync 透传 argv、stdio 与退出码；子包缺失（--omit=optional 或无预编译平台）时输出重装/下载提示并退出 1。整个 npm 分发只有这一份运行时 JS

[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
