#!/usr/bin/env node
import { createRequire } from 'node:module'
import { existsSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { spawnSync } from 'node:child_process'

const require = createRequire(import.meta.url)
const here = dirname(fileURLToPath(import.meta.url))
const root = dirname(here)

const targets = {
  'darwin-arm64': {
    packageName: 'uapi-cli-darwin-arm64',
    relativeDevPath: join(root, '..', 'uapi-cli-darwin-arm64', 'bin', 'uapi'),
  },
  'darwin-x64': {
    packageName: 'uapi-cli-darwin-x64',
    relativeDevPath: join(root, '..', 'uapi-cli-darwin-x64', 'bin', 'uapi'),
  },
  'linux-arm64': {
    packageName: 'uapi-cli-linux-arm64',
    relativeDevPath: join(root, '..', 'uapi-cli-linux-arm64', 'bin', 'uapi'),
  },
  'linux-x64': {
    packageName: 'uapi-cli-linux-x64',
    relativeDevPath: join(root, '..', 'uapi-cli-linux-x64', 'bin', 'uapi'),
  },
  'win32-x64': {
    packageName: 'uapi-cli-win32-x64',
    relativeDevPath: join(root, '..', 'uapi-cli-win32-x64', 'bin', 'uapi.exe'),
  },
}

const target = targets[`${process.platform}-${process.arch}`]
if (!target) {
  console.error(`Unsupported platform: ${process.platform}-${process.arch}`)
  process.exit(1)
}

function resolveBinary() {
  if (existsSync(target.relativeDevPath)) {
    return target.relativeDevPath
  }
  try {
    const pkgJson = require.resolve(`${target.packageName}/package.json`)
    return join(dirname(pkgJson), 'bin', process.platform === 'win32' ? 'uapi.exe' : 'uapi')
  } catch {
    console.error(`Missing platform package ${target.packageName}. Run npm install or npm run build:release.`)
    process.exit(1)
  }
}

const binary = resolveBinary()
const result = spawnSync(binary, process.argv.slice(2), {
  stdio: 'inherit',
})

if (result.error) {
  console.error(result.error.message)
  process.exit(1)
}

process.exit(result.status ?? 0)
