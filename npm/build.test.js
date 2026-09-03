/**
 * [INPUT]: 依赖 node:test / node:assert；被测 ./build.js
 * [OUTPUT]: 单元测试，无导出
 * [POS]: 覆盖 buildManifests 的平台映射、发布顺序、optionalDependencies 精确钉版，以及 parseArgs 的版本校验
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

'use strict'

const test = require('node:test')
const assert = require('node:assert/strict')
const { buildManifests, platformPackageName, parseArgs } = require('./build.js')

const binaries = [
  { path: 'dist/a/makecli', goos: 'darwin', goarch: 'arm64' },
  { path: 'dist/b/makecli.exe', goos: 'windows', goarch: 'amd64' },
  { path: 'dist/c/makecli', goos: 'linux', goarch: 'amd64' },
]

test('platform packages map goos/goarch to node os/cpu and ship the binary', () => {
  const [darwin, win] = buildManifests('1.2.3', binaries, '/src')
  assert.equal(darwin.packageJson.name, '@qfeius/makecli-darwin-arm64')
  assert.deepEqual(darwin.packageJson.os, ['darwin'])
  assert.deepEqual(darwin.packageJson.cpu, ['arm64'])
  assert.deepEqual(darwin.files, [{ src: 'dist/a/makecli', dst: 'bin/makecli', mode: 0o755 }])

  assert.equal(win.packageJson.name, '@qfeius/makecli-win32-x64')
  assert.equal(win.files[0].dst, 'bin/makecli.exe')
})

test('main package comes last and pins every platform package to the same version', () => {
  const manifests = buildManifests('1.2.3-beta.1', binaries, '/src')
  const main = manifests.at(-1)
  assert.equal(main.packageJson.name, '@qfeius/makecli')
  assert.equal(main.packageJson.version, '1.2.3-beta.1')
  assert.deepEqual(main.packageJson.bin, { makecli: 'bin/makecli.js' })
  assert.deepEqual(main.packageJson.optionalDependencies, {
    '@qfeius/makecli-darwin-arm64': '1.2.3-beta.1',
    '@qfeius/makecli-linux-x64': '1.2.3-beta.1',
    '@qfeius/makecli-win32-x64': '1.2.3-beta.1',
  })
  assert.equal(manifests.length, binaries.length + 1)
  for (const m of manifests) assert.deepEqual(m.packageJson.publishConfig, { access: 'public' })
})

test('unsupported target is rejected instead of silently skipped', () => {
  assert.throws(() => buildManifests('1.0.0', [{ path: 'x', goos: 'plan9', goarch: 'amd64' }], '/src'), /unsupported target plan9\/amd64/)
})

test('platformPackageName', () => {
  assert.equal(platformPackageName('linux', 'arm64'), '@qfeius/makecli-linux-arm64')
})

test('parseArgs requires a bare semver version (no v prefix)', () => {
  assert.deepEqual(parseArgs(['--version', '0.5.9']), { dist: 'dist', out: 'dist/npm', version: '0.5.9' })
  assert.equal(parseArgs(['--version', '0.6.0-beta.2', '--out', 'x']).out, 'x')
  assert.throws(() => parseArgs(['--version', 'v0.5.9']), /invalid semver/)
  assert.throws(() => parseArgs(['--bogus', '1']), /usage/)
})
