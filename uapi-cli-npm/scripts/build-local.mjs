import { spawnSync } from 'node:child_process'
import { mkdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const root = dirname(here)
const repoRoot = dirname(root)
const cliRoot = join(repoRoot, 'uapi-cli')

const map = {
  'win32-x64': { dir: 'uapi-cli-win32-x64', binary: 'uapi.exe' },
  'linux-x64': { dir: 'uapi-cli-linux-x64', binary: 'uapi' },
  'linux-arm64': { dir: 'uapi-cli-linux-arm64', binary: 'uapi' },
  'darwin-x64': { dir: 'uapi-cli-darwin-x64', binary: 'uapi' },
  'darwin-arm64': { dir: 'uapi-cli-darwin-arm64', binary: 'uapi' },
}

const target = map[`${process.platform}-${process.arch}`]
if (!target) {
  console.error(`Unsupported platform: ${process.platform}-${process.arch}`)
  process.exit(1)
}

const outFile = join(repoRoot, target.dir, 'bin', target.binary)
mkdirSync(dirname(outFile), { recursive: true })
const result = spawnSync(
  'go',
  ['build', '-trimpath', '-ldflags', '-s -w', '-o', outFile, '.'],
  {
    cwd: cliRoot,
    stdio: 'inherit',
    env: {
      ...process.env,
      CGO_ENABLED: '0',
    },
  },
)

process.exit(result.status ?? 0)
