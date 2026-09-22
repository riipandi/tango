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

  // Template file names awaiting a dev rebuild; empty means "compile everything".
  const pending = new Set<string>()

  // Compiles one template into its _html and _text pair. Returns the template
  // name on success, null on failure (already logged).
  async function buildOne(
    absTemplates: string,
    absOutput: string,
    file: string
  ): Promise<string | null> {
    const templateName = file.replace('.tsx', '')
    const start = Date.now()

    try {
      const importedModule = await importTemplate(path.join(absTemplates, file), Date.now())
      const Component = (importedModule.default ?? getFirstExport(importedModule)) as EmailTemplate

      if (!Component) {
        throw new Error('no component export found')
      }

      if (!Component.TemplateProps) {
        throw new Error('no TemplateProps export found')
      }

      await buildTemplateFile(Component, Component.TemplateProps, templateName, absOutput, false) // HTML
      await buildTemplateFile(Component, Component.TemplateProps, templateName, absOutput, true) // Text

      log(`  ${C.green}✓ ${templateName}${C.reset} in ${formatDuration(Date.now() - start)}`)
      return templateName
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error)
      logError(`  ✗ ${templateName}: ${message}`)
      return null
    }
  }

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
    const files = fs.readdirSync(absTemplates).filter((file) => file.endsWith('.tsx'))
    let built = 0
    let failed = 0

    for (const file of files) {
      const name = await buildOne(absTemplates, absOutput, file)
      if (name === null) {
        failed++
      } else {
        built++
      }
    }

    const duration = formatDuration(Date.now() - startedAt)

    if (built > 0) {
      log(
        `${C.green}built ${built}/${files.length} templates → ${opts.outputDir} in ${duration}${C.reset}\n`
      )
    } else if (files.length === 0) {
      log(`no templates found in ${opts.templateDir}`)
    }

    return failed === 0
  }

  // A dev rebuild touches only the templates that changed; the untouched
  // compiled pairs keep their files, so the Go embed sees no churn.
  async function rebuildChanged(): Promise<boolean> {
    const absTemplates = path.resolve(viteRoot, opts.templateDir)
    const absOutput = path.resolve(viteRoot, opts.outputDir)
    const files = [...pending]
    pending.clear()

    const startedAt = Date.now()
    let built = 0
    let ok = true

    for (const file of files) {
      const name = await buildOne(absTemplates, absOutput, file)
      if (name === null) {
        ok = false
      } else {
        built++
      }
    }

    if (built > 0) {
      log(
        `${C.green}rebuilt ${built}/${files.length} template(s) in ${formatDuration(Date.now() - startedAt)}${C.reset}`
      )
    }
    return ok
  }

  async function rebuild() {
    if (isBuilding) {
      hasPendingChanges = true
      return
    }

    isBuilding = true
    const ok = pending.size === 0 ? await buildAll() : await rebuildChanged()
    isBuilding = false

    if (!ok) return

    if (hasPendingChanges) {
      hasPendingChanges = false
      await rebuild()
    }
  }

  function schedule(file?: string) {
    if (file) pending.add(path.basename(file))
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
        schedule(file)
      }

      server.watcher.on('change', onFile)
      server.watcher.on('add', onFile)
      // A removed template cannot be rebuilt alone; a full pass reconciles the
      // compiled pairs with what is on disk.
      server.watcher.on('unlink', (file: string) => {
        if (disposed || !shouldWatch(file)) return
        pending.clear()
        schedule()
      })

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
