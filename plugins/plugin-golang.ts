import { spawn, type ChildProcess } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import type { Plugin, ViteDevServer } from 'vite'

export interface GoBuildOptions {
  args?: string[]
  packagePath?: string
  outputDir?: string
  outputBin?: string
  embedDir?: string
  buildTags?: string[]
  buildFlags?: string[]
  ldflags?: string[]
}

export interface PluginGolangOptions {
  packageName: string
  cmd?: string
  args?: string[]
  packagePath?: string
  bin?: string
  binArgs?: string[]
  delay?: number
  killDelay?: number
  stopOnError?: boolean
  excludeDir?: string[]
  excludeRegex?: string[]
  extensions?: string[]
  log?: boolean
  build?: GoBuildOptions
}

interface ResolvedBuildOptions {
  outputDir: string
  outputBin: string
  embedDir: string
  args: string[]
  buildTags: string[]
  buildFlags: string[]
  ldflags: string[]
}

interface GoPluginDefaults {
  cmd: string
  binArgs: string[]
  delay: number
  killDelay: number
  stopOnError: boolean
  excludeDir: string[]
  excludeRegex: string[]
  extensions: string[]
  log: boolean
  build: Required<GoBuildOptions>
}

const defaults: GoPluginDefaults = {
  cmd: 'go',
  binArgs: [],
  delay: 1000,
  killDelay: 300,
  stopOnError: false,
  excludeDir: ['.git', 'vendor', 'node_modules', 'web/output', 'temp', 'tmp', 'build', 'dist'],
  excludeRegex: ['_test\\.go$'],
  extensions: ['go', 'tmpl'],
  log: true,
  build: {
    args: [],
    packagePath: '.',
    outputDir: '',
    outputBin: '',
    embedDir: 'web/output',
    buildTags: [],
    buildFlags: [],
    ldflags: []
  }
}

const C = {
  reset: '\x1b[0m',
  dim: '\x1b[2m',
  green: '\x1b[32m',
  red: '\x1b[31m',
  cyan: '\x1b[36m'
} as const

const PREFIX = `${C.cyan}[go]${C.reset}`

const isProduction = () => process.env.NODE_ENV === 'production'

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

function formatFileSize(bytes: number): string {
  if (bytes >= 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(2)} MB`
  return `${(bytes / 1024).toFixed(1)} KB`
}

function formatBuildInfo(buildOpts: ResolvedBuildOptions): Array<{ label: string; value: string }> {
  const mode = isProduction() || buildOpts.buildTags.includes('release') ? 'release' : 'debug'
  const tags = buildOpts.buildTags.length > 0 ? buildOpts.buildTags.join(', ') : 'none'

  const lines: Array<{ label: string; value: string }> = [
    { label: 'mode', value: mode },
    { label: 'tags', value: tags }
  ]

  if (buildOpts.buildFlags.length > 0) {
    lines.push({ label: 'flags', value: buildOpts.buildFlags.join(' ') })
  }

  for (const [index, flag] of buildOpts.ldflags.entries()) {
    lines.push({ label: `ldflags[${index}]`, value: flag })
  }

  lines.push({ label: 'embed', value: buildOpts.embedDir })
  lines.push({ label: 'output', value: `${buildOpts.outputDir}/${buildOpts.outputBin}` })

  return lines
}

function resolveBuildOptions(
  userBuild: GoBuildOptions | undefined,
  userPkg: string,
  packageName: string
): ResolvedBuildOptions {
  const outputDir = userBuild?.outputDir || (isProduction() ? 'build/release' : 'build/debug')
  const outputBin = userBuild?.outputBin || packageName
  const embedDir = userBuild?.embedDir || 'web/output'
  const buildFlags = userBuild?.buildFlags || []
  const ldflags = userBuild?.ldflags || []
  const buildTags = userBuild?.buildTags || defaults.build.buildTags
  const pkg = userBuild?.packagePath || userPkg

  const buildArgs =
    userBuild?.args && userBuild.args.length > 0
      ? [...userBuild.args]
      : ['build', '-o', `${outputDir}/${outputBin}`, pkg]

  if (buildTags.length > 0) {
    buildArgs.splice(1, 0, '-tags', buildTags.join(','))
  }

  if (buildFlags.length > 0) {
    buildArgs.splice(1, 0, ...buildFlags)
  }

  if (ldflags.length > 0) {
    buildArgs.splice(1, 0, '-ldflags', ldflags.join(' '))
  }

  return { outputDir, outputBin, embedDir, args: buildArgs, buildFlags, ldflags, buildTags }
}

interface GoBuildResult {
  code: number | null
  output: string
  duration: number
}

function runGoBuild(cmd: string, args: string[], cwd: string): Promise<GoBuildResult> {
  return new Promise((resolve) => {
    const startTime = Date.now()
    const buildProcess = spawn(cmd, args, { stdio: 'pipe', cwd })
    let output = ''

    buildProcess.stdout?.on('data', (data: Buffer) => {
      output += data.toString()
    })

    buildProcess.stderr?.on('data', (data: Buffer) => {
      output += data.toString()
    })

    buildProcess.on('close', (code) => {
      resolve({ code, output, duration: Date.now() - startTime })
    })
  })
}

export default function VitePlugin(userOptions: PluginGolangOptions): Plugin {
  if (process.env.VITEST || process.env.STORYBOOK || process.env.TANGO_SKIP_GO) {
    return { name: 'vite-plugin-go' }
  }

  const name = userOptions.packageName
  const pkgPath = userOptions.packagePath || defaults.build.packagePath
  const defaultBin = `build/debug/${name}`
  const defaultArgs = ['build', '-o', defaultBin, pkgPath]

  const opts = {
    ...defaults,
    cmd: userOptions.cmd ?? defaults.cmd,
    args: userOptions.args ?? defaultArgs,
    bin: userOptions.bin || defaultBin,
    binArgs: userOptions.binArgs ?? defaults.binArgs,
    delay: userOptions.delay ?? defaults.delay,
    killDelay: userOptions.killDelay ?? defaults.killDelay,
    stopOnError: userOptions.stopOnError ?? defaults.stopOnError,
    excludeDir: userOptions.excludeDir ?? defaults.excludeDir,
    excludeRegex: userOptions.excludeRegex ?? defaults.excludeRegex,
    extensions: userOptions.extensions ?? defaults.extensions,
    log: userOptions.log ?? defaults.log,
    build: { ...defaults.build, ...userOptions.build }
  }

  const excludePatterns = [
    ...opts.excludeDir.map((d) => new RegExp(`[\\/]${path.normalize(d)}[\\/]`)),
    ...opts.excludeRegex.map((r) => new RegExp(r))
  ]

  const buildOpts = resolveBuildOptions(userOptions.build, pkgPath, name)

  let viteRoot = process.cwd()
  let command: 'serve' | 'build' = 'serve'
  let goProcess: ChildProcess | null = null
  let buildTimer: ReturnType<typeof setTimeout> | null = null
  let isBuilding = false
  let hasPendingChanges = false
  let onSignal: (() => void) | null = null
  let disposed = false

  function log(msg: string) {
    if (opts.log) console.log(`${PREFIX} ${msg}`)
  }

  function logInfo(label: string, value: string, width?: number) {
    // Default gutter fits the longest common label; callers may pass an exact width.
    const w = width ?? (label.endsWith(':') ? 10 : 9)
    log(`${C.dim}${label.padEnd(w)}${C.reset}${value}`)
  }

  function logOutput(output: string) {
    for (const line of output.split('\n')) {
      if (line.trim()) console.error(`${PREFIX} ${C.red}${line}${C.reset}`)
    }
  }

  function killGo() {
    if (!goProcess) return
    const proc = goProcess
    goProcess = null
    try {
      process.kill(-proc.pid!, 'SIGTERM')
    } catch {
      proc.kill('SIGTERM')
    }
  }

  function dispose() {
    if (disposed) return
    disposed = true
    if (buildTimer) clearTimeout(buildTimer)
    killGo()
    if (onSignal) {
      process.removeListener('SIGINT', onSignal)
      process.removeListener('SIGTERM', onSignal)
      onSignal = null
    }
    log('stopped')
  }

  function startBinary() {
    const binPath = path.resolve(viteRoot, opts.bin)
    const proc = spawn(binPath, opts.binArgs, {
      cwd: viteRoot,
      stdio: 'inherit',
      detached: true
    })
    goProcess = proc

    proc.on('error', (err) => {
      log(`failed to start binary: ${err.message}`)
      goProcess = null
    })

    proc.on('exit', (code, signal) => {
      // goProcess was already cleared by killGo() → expected shutdown, no log.
      if (goProcess !== proc) return
      goProcess = null
      if (!disposed) log(`binary exited unexpectedly (${signal ?? `exit code ${code}`})`)
    })

    if (proc.pid) log(`started (pid ${proc.pid})`)
  }

  async function runBuild(stage: 'initial' | 'rebuild'): Promise<boolean> {
    isBuilding = true
    log(stage === 'rebuild' ? 'rebuilding...' : 'building debug binary...')

    const { code, output, duration } = await runGoBuild(opts.cmd, opts.args, viteRoot)
    isBuilding = false

    if (code !== 0) {
      log(`${C.red}build failed (exit code ${code}) in ${formatDuration(duration)}${C.reset}`)
      logOutput(output)
      return false
    }

    log(
      `${C.green}${stage === 'rebuild' ? 'rebuilt' : 'built'} in ${formatDuration(duration)}${C.reset}`
    )
    return true
  }

  function buildAndStart() {
    if (isBuilding || disposed) return
    if (buildTimer) clearTimeout(buildTimer)

    buildTimer = setTimeout(() => {
      void runBuild('rebuild').then((ok) => {
        buildTimer = null

        if (!ok && opts.stopOnError) killGo()

        if (hasPendingChanges) {
          hasPendingChanges = false
          buildAndStart()
          return
        }

        if (ok) {
          setTimeout(() => {
            if (disposed) return
            killGo()
            startBinary()
          }, opts.killDelay)
        }
      })
    }, opts.delay)
  }

  async function initialBuild() {
    const ok = await runBuild('initial')
    if (disposed || !ok) return
    startBinary()
  }

  function shouldWatch(filePath: string): boolean {
    const ext = path.extname(filePath).slice(1)
    if (!opts.extensions.includes(ext)) return false
    if (excludePatterns.some((re) => re.test(filePath))) return false
    return true
  }

  return {
    name: 'vite-plugin-go',
    configResolved(config) {
      viteRoot = config.root
      command = config.command
    },
    configureServer(_server: ViteDevServer) {
      void initialBuild()

      _server.watcher.on('change', (file: string) => {
        if (disposed) return
        if (!shouldWatch(file)) return

        if (isBuilding) {
          hasPendingChanges = true
          return
        }

        buildAndStart()
      })

      onSignal = () => {
        dispose()
        process.exit(0)
      }
      process.on('SIGINT', onSignal)
      process.on('SIGTERM', onSignal)
    },
    closeBundle: {
      sequential: true,
      order: 'post',
      async handler() {
        // Vite also fires closeBundle when the dev server shuts down or
        // restarts — only run the production build for `vite build`.
        if (command !== 'build') {
          dispose()
          return
        }

        const embedPath = path.resolve(viteRoot, buildOpts.embedDir)
        if (!fs.existsSync(embedPath)) {
          log(`embed directory "${buildOpts.embedDir}" not found, skipping go build`)
          process.exitCode = 1
          return
        }

        fs.mkdirSync(path.resolve(viteRoot, buildOpts.outputDir), { recursive: true })

        const buildMode = buildOpts.buildTags.includes('debug') ? 'debug' : 'release'
        log(`building binary (${buildMode})...`)

        const infoLines = formatBuildInfo(buildOpts)
        const gutter = Math.max(...infoLines.map((line) => line.label.length)) + 1
        for (const line of infoLines) {
          logInfo(line.label, line.value, gutter)
        }

        const { code, output, duration } = await runGoBuild(opts.cmd, buildOpts.args, viteRoot)
        const binPath = `${buildOpts.outputDir}/${buildOpts.outputBin}`

        if (code !== 0) {
          log(`${C.red}build failed (exit code ${code}) in ${formatDuration(duration)}${C.reset}`)
          logOutput(output)
          process.exitCode = 1
          return
        }

        let size = ''
        try {
          size = ` (${formatFileSize(fs.statSync(path.resolve(viteRoot, binPath)).size)})`
        } catch {
          // binary missing after a successful build is highly unlikely; keep summary short
        }

        log(`${C.green}binary built → ${binPath}${size} in ${formatDuration(duration)}${C.reset}`)
      }
    }
  }
}
