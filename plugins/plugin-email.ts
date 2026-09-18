import * as fs from 'node:fs'
import * as path from 'node:path'
import { pathToFileURL } from 'node:url'
import type { ReactElement, ReactNode } from 'react'
import { render } from 'react-email'
import type { Plugin } from 'vite'

type EmailTemplate = ((props: unknown) => ReactNode) & { TemplateProps?: unknown }

export interface PluginEmailOptions {
  /** Directory containing the React Email templates (default: "email/templates"). */
  templateDir?: string
  /** Directory the compiled Go templates are written to (default: "web/email"). */
  outputDir?: string
  /** Recompile on template change in dev (default: true). */
  watch?: boolean
  /** Debounce for template changes in ms (default: 300). */
  delay?: number
  log?: boolean
}

interface PluginEmailDefaults {
  templateDir: string
  outputDir: string
  watch: boolean
  delay: number
  log: boolean
}

const defaults: PluginEmailDefaults = {
  templateDir: 'email/templates',
  outputDir: 'web/email',
  watch: true,
  delay: 300,
  log: true
}

// Log style kept in sync with plugins/plugin-golang.ts
const C = {
  reset: '\x1b[0m',
  dim: '\x1b[2m',
  green: '\x1b[32m',
  red: '\x1b[31m',
  cyan: '\x1b[36m'
} as const

const PREFIX = `${C.cyan}[email]${C.reset}`

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`
  return `${(ms / 1000).toFixed(1)}s`
}

function log(msg: string) {
  console.log(`${PREFIX} ${msg}`)
}

function logInfo(label: string, value: string) {
  console.log(`${PREFIX} ${C.dim}${label.padEnd(10)}${C.reset}${value}`)
}

function logError(msg: string) {
  console.error(`${PREFIX} ${C.red}${msg}${C.reset}`)
}

function getFirstExport(module: Record<string, unknown>): unknown {
  const keys = Object.keys(module)
  if (keys.length === 0) return undefined
  const firstKey = keys[0]
  if (firstKey === undefined) return undefined
  return module[firstKey]
}

// Template files are .tsx (JSX), so they must go through a TypeScript
// transform before we can import them in-process. tsx is already a
// devDependency; its ESM API registers the loader for the current thread.
async function importTemplate(file: string, cacheBust: number): Promise<Record<string, unknown>> {
  const { tsImport } = await import('tsx/esm/api')
  const mod = await tsImport(`${pathToFileURL(file).href}?t=${cacheBust}`, import.meta.url)
  return mod as Record<string, unknown>
}

async function buildTemplateFile(
  Component: (props: unknown) => ReactNode,
  templateProps: unknown,
  templateName: string,
  outputDir: string,
  isPlainText: boolean
) {
  const element = Component(templateProps) as ReactElement

  // `plainText` is a discriminated union, so it must be a literal, not a boolean.
  const rendered = isPlainText
    ? await render(element, { plainText: true })
    : await render(element, { plainText: false })

  // Normalize quotes
  const normalized = rendered.replace(/&quot;/g, '"')

  const goTemplate = `{{define "root"}}${normalized}{{end}}`
  const suffix = isPlainText ? '_text.tmpl' : '_html.tmpl'
  const templatePath = path.join(outputDir, `${templateName}${suffix}`)

  fs.writeFileSync(templatePath, goTemplate)
}

export default function VitePluginEmail(userOptions: PluginEmailOptions = {}): Plugin {
  if (process.env.VITEST || process.env.STORYBOOK) {
    return { name: 'vite-plugin-email' }
  }

  const opts: PluginEmailDefaults = { ...defaults, ...userOptions }

  let viteRoot = process.cwd()
  let command: 'serve' | 'build' = 'serve'
  let timer: ReturnType<typeof setTimeout> | null = null
  let isBuilding = false
  let hasPendingChanges = false
  let disposed = false

  async function buildAll(): Promise<boolean> {
    const absTemplates = path.resolve(viteRoot, opts.templateDir)
    const absOutput = path.resolve(viteRoot, opts.outputDir)

    if (!fs.existsSync(absTemplates)) {
      logError(`templates directory not found: ${opts.templateDir}`)
      return false
    }

    fs.mkdirSync(absOutput, { recursive: true })

    log('building email templates...')
    logInfo('templates', opts.templateDir)
    logInfo('output', opts.outputDir)

    const startedAt = Date.now()
    const cacheBust = startedAt
    let built = 0
    let failed = 0

    for (const file of fs.readdirSync(absTemplates)) {
      if (!file.endsWith('.tsx')) continue

      const templateName = file.replace('.tsx', '')
      const start = Date.now()

      try {
        const importedModule = await importTemplate(path.join(absTemplates, file), cacheBust)
        const Component = (importedModule.default ??
          getFirstExport(importedModule)) as EmailTemplate

        if (!Component) {
          throw new Error('no component export found')
        }

        if (!Component.TemplateProps) {
          throw new Error('no TemplateProps export found')
        }

        await buildTemplateFile(Component, Component.TemplateProps, templateName, absOutput, false) // HTML
        await buildTemplateFile(Component, Component.TemplateProps, templateName, absOutput, true) // Text

        built++
        log(`  ${C.green}✓ ${templateName}${C.reset} in ${formatDuration(Date.now() - start)}`)
      } catch (error) {
        failed++
        const message = error instanceof Error ? error.message : String(error)
        logError(`  ✗ ${templateName}: ${message}`)
      }
    }

    const total = built + failed
    const duration = formatDuration(Date.now() - startedAt)

    if (built > 0) {
      log(
        `${C.green}built ${built}/${total} templates → ${opts.outputDir} in ${duration}${C.reset}`
      )
    } else if (total === 0) {
      log(`no templates found in ${opts.templateDir}`)
    }

    return failed === 0
  }

  async function rebuild() {
    if (isBuilding) {
      hasPendingChanges = true
      return
    }

    isBuilding = true
    await buildAll()
    isBuilding = false

    if (hasPendingChanges) {
      hasPendingChanges = false
      await rebuild()
    }
  }

  function schedule() {
    if (timer) clearTimeout(timer)
    timer = setTimeout(() => {
      timer = null
      void rebuild()
    }, opts.delay)
  }

  function cleanup() {
    if (disposed) return
    disposed = true
    if (timer) clearTimeout(timer)
    timer = null
  }

  function shouldWatch(filePath: string): boolean {
    if (!filePath.endsWith('.tsx')) return false
    const rel = path.relative(path.resolve(viteRoot, opts.templateDir), filePath)
    return !rel.startsWith('..')
  }

  return {
    name: 'vite-plugin-email',
    configResolved(config) {
      viteRoot = config.root
      command = config.command
    },
    configureServer(server) {
      void rebuild()

      if (!opts.watch) return

      log(`watching ${opts.templateDir} for changes...`)

      const onFile = (file: string) => {
        if (disposed || !shouldWatch(file)) return
        schedule()
      }

      server.watcher.on('change', onFile)
      server.watcher.on('add', onFile)
      server.watcher.on('unlink', onFile)

      server.httpServer?.once('close', cleanup)
    },
    closeBundle: {
      // Runs before plugin-golang's closeBundle so the Go binary always
      // embeds freshly compiled templates (web/embed.go: email/*.tmpl).
      sequential: true,
      order: 'pre',
      async handler() {
        if (command !== 'build') {
          // Dev server shutdown/restart — just drop pending timers.
          cleanup()
          return
        }

        const ok = await buildAll()
        if (!ok) {
          logError('email template build failed, aborting go binary build')
          process.exitCode = 1
          throw new Error('email template build failed')
        }
      }
    }
  }
}
