#!/usr/bin/env node
/**
 * [INPUT]: 依赖 node:child_process；运行时依赖已安装的 @qfeius/makecli-<platform>-<arch> 平台子包
 * [OUTPUT]: 主包 @qfeius/makecli 的 bin 入口，把调用透传给平台子包内的 Go 二进制
 * [POS]: npm 分发的唯一 JS 代码——不下载、不解压、不校验，全部交给 npm 的 optionalDependencies 机制；
 *        找不到子包即报错退出（用户用 --omit=optional 装的、或平台无预编译）
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

'use strict'

const { spawnSync } = require('node:child_process')

const pkg = `@qfeius/makecli-${process.platform}-${process.arch}`
const bin = process.platform === 'win32' ? 'bin/makecli.exe' : 'bin/makecli'

let binaryPath
try {
  binaryPath = require.resolve(`${pkg}/${bin}`)
} catch {
  console.error(
    `makecli: platform package ${pkg} is not installed.\n` +
    `Reinstall without --omit=optional, or download a binary from https://github.com/qfeius/makecli/releases`,
  )
  process.exit(1)
}

const result = spawnSync(binaryPath, process.argv.slice(2), { stdio: 'inherit' })
if (result.error) {
  console.error(`makecli: ${result.error.message}`)
  process.exit(1)
}
process.exit(result.status ?? 1)
