/**
 * [INPUT]: 依赖 node:fs / node:path；输入 GoReleaser 产出的 dist/artifacts.json 与其中的 Binary 条目
 * [OUTPUT]: 导出 buildManifests / platformPackageName / parseArgs 供测试；作为 CLI 运行时在 --out 下写出 7 个可 publish 的包目录，stdout 按发布顺序逐行打印目录（平台子包在前、主包最后）
 * [POS]: 发布流水线的 npm 打包步骤（release.yml 调用）；平台子包携带二进制并声明 os/cpu，主包用 optionalDependencies 精确钉住同版本子包
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */

'use strict'

const fs = require('node:fs')
const path = require('node:path')

const SCOPE = '@qfeius'
const NAME = 'makecli'
const REPO = 'https://github.com/qfeius/makecli'

// ---------------------------------------------------------------------------
// GoReleaser 平台名 → Node 平台名
// ---------------------------------------------------------------------------
const OS = { darwin: 'darwin', linux: 'linux', windows: 'win32' }
const CPU = { amd64: 'x64', arm64: 'arm64' }

const common = (version) => ({
  version,
  license: 'MIT',
  repository: { type: 'git', url: `git+${REPO}.git` },
  homepage: REPO,
  bugs: `${REPO}/issues`,
  publishConfig: { access: 'public' },
})

function platformPackageName(os, cpu) {
  return `${SCOPE}/${NAME}-${os}-${cpu}`
}

// buildManifests 把 Binary 条目映射为「包目录 + package.json + 需拷贝的文件」，纯函数便于测试。
// binaries: [{ path, goos, goarch }]；返回顺序即发布顺序。
function buildManifests(version, binaries, srcDir) {
  const platforms = binaries.map((b) => {
    const os = OS[b.goos]
    const cpu = CPU[b.goarch]
    if (!os || !cpu) throw new Error(`unsupported target ${b.goos}/${b.goarch}`)
    const name = platformPackageName(os, cpu)
    const binName = os === 'win32' ? 'makecli.exe' : 'makecli'
    return {
      dir: `${NAME}-${os}-${cpu}`,
      packageJson: {
        name,
        description: `makecli prebuilt binary for ${os}-${cpu}`,
        os: [os],
        cpu: [cpu],
        ...common(version),
      },
      files: [{ src: b.path, dst: `bin/${binName}`, mode: 0o755 }],
    }
  })

  const optionalDependencies = Object.fromEntries(
    platforms.map((p) => [p.packageJson.name, version]).sort(),
  )

  const main = {
    dir: NAME,
    packageJson: {
      name: `${SCOPE}/${NAME}`,
      description: 'makecli — agentic development platform cli',
      bin: { makecli: 'bin/makecli.js' },
      optionalDependencies,
      ...common(version),
    },
    files: [
      { src: path.join(srcDir, 'bin', 'makecli.js'), dst: 'bin/makecli.js', mode: 0o755 },
      { src: path.join(srcDir, 'README.md'), dst: 'README.md' },
      { src: path.join(srcDir, '..', 'LICENSE'), dst: 'LICENSE' },
    ],
  }

  return [...platforms, main]
}

function writePackage(outDir, manifest) {
  const dir = path.join(outDir, manifest.dir)
  fs.rmSync(dir, { recursive: true, force: true })
  fs.mkdirSync(dir, { recursive: true })
  fs.writeFileSync(path.join(dir, 'package.json'), JSON.stringify(manifest.packageJson, null, 2) + '\n')
  for (const f of manifest.files) {
    const dst = path.join(dir, f.dst)
    fs.mkdirSync(path.dirname(dst), { recursive: true })
    fs.copyFileSync(f.src, dst)
    if (f.mode) fs.chmodSync(dst, f.mode)
  }
  return dir
}

function parseArgs(argv) {
  const opts = { dist: 'dist', out: 'dist/npm', version: '' }
  for (let i = 0; i < argv.length; i += 2) {
    const key = argv[i].replace(/^--/, '')
    if (!(key in opts) || argv[i + 1] === undefined) throw new Error(`usage: build.js --version X.Y.Z [--dist dist] [--out dist/npm]`)
    opts[key] = argv[i + 1]
  }
  if (!/^\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?$/.test(opts.version)) throw new Error(`invalid semver version: ${JSON.stringify(opts.version)}`)
  return opts
}

function main() {
  const opts = parseArgs(process.argv.slice(2))
  const artifacts = JSON.parse(fs.readFileSync(path.join(opts.dist, 'artifacts.json'), 'utf8'))
  const binaries = artifacts.filter((a) => a.type === 'Binary')
  if (binaries.length === 0) throw new Error(`no Binary artifacts in ${opts.dist}/artifacts.json`)
  for (const m of buildManifests(opts.version, binaries, __dirname)) {
    process.stdout.write(writePackage(opts.out, m) + '\n')
  }
}

if (require.main === module) main()

module.exports = { buildManifests, platformPackageName, parseArgs }
