import { mkdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawnSync } from 'node:child_process'

const here = dirname(fileURLToPath(import.meta.url))
const root = dirname(here)
const repoRoot = dirname(root)
const cliRoot = join(repoRoot, 'uapi-cli')

const targets = [
  { goos: 'windows', goarch: 'amd64', dir: 'uapi-cli-win32-x64', binary: 'uapi.exe' },
  { goos: 'linux', goarch: 'amd64', dir: 'uapi-cli-linux-x64', binary: 'uapi' },
  { goos: 'linux', goarch: 'arm64', dir: 'uapi-cli-linux-arm64', binary: 'uapi' },
  { goos: 'darwin', goarch: 'amd64', dir: 'uapi-cli-darwin-x64', binary: 'uapi' },
  { goos: 'darwin', goarch: 'arm64', dir: 'uapi-cli-darwin-arm64', binary: 'uapi' },
]

for (const target of targets) {
  const outDir = join(repoRoot, target.dir, 'bin')
  mkdirSync(outDir, { recursive: true })
  const outFile = join(outDir, target.binary)
  const result = spawnSync(
    'go',
    ['build', '-trimpath', '-ldflags', '-s -w', '-o', outFile, '.'],
    {
      cwd: cliRoot,
      stdio: 'inherit',
      env: {
        ...process.env,
        GOOS: target.goos,
        GOARCH: target.goarch,
        CGO_ENABLED: '0',
      },
    },
  )
  if (result.status !== 0) {
    process.exit(result.status ?? 1)
  }
}
