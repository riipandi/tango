import { resolve } from 'node:path'
import { defineConfig, type ProxyOptions } from 'vite'
import { comlink } from 'vite-plugin-comlink'
import pkg from './package.json' with { type: 'json' }
import email from './plugins/plugin-email.ts'
import golang from './plugins/plugin-golang.ts'

// Must match the module path in go.mod.
const goModule = 'github.com/riipandi/tango'

// const isProduction = process.env.NODE_ENV === "production";
// const isTestOrCI = process.env.CI || process.env.VITEST
// const isVitest = process.env.VITEST
const isStorybook = process.env.STORYBOOK === 'true'
const APP_VERSION = process.env.BUILD_VERSION || pkg.version
const BUILD_DATE = process.env.BUILD_DATE || new Date().toISOString()
const BUILD_HASH = process.env.BUILD_HASH || 'dev'

// Version stamps shared by every Go target; release adds its static-link flags.
const goVersionLdflags = [
  `-X ${goModule}/internal/config.AppVersion=${APP_VERSION}`,
  `-X ${goModule}/internal/config.BuildHash=${BUILD_HASH}`,
  `-X ${goModule}/internal/config.BuildDate=${BUILD_DATE}`
]

// Same-origin proxy to the demo auth backend. Required for the HttpOnly
// cookie session: cookies default to SameSite=Lax, which is not sent on
// cross-site fetches, and this keeps them first-party.
const viteProxy: Record<string, string | ProxyOptions> = {
  '/.well-known': { target: 'http://127.0.0.1:3080', changeOrigin: true },
  '/api': { target: 'http://127.0.0.1:3080', changeOrigin: true },
  '/rpc': { target: 'http://127.0.0.1:3080', changeOrigin: true },
  '/metrics': { target: 'http://127.0.0.1:3080', changeOrigin: true },
  '/static': { target: 'http://127.0.0.1:3080', changeOrigin: true }
}

export default defineConfig({
  plugins: [
    // Comlink owns worker construction and must register first: only
    // plugins that transform ComlinkWorker call sites may precede it.
    comlink(),
    // Must be registered before the go plugin: its closeBundle compiles
    // the email templates that web/embed.go pulls into the go binary.
    email({ templateDir: 'email/templates', outputDir: 'web/email' }),
    // One `vite build` produces every target; the dev server builds and runs
    // only the dev target (debug). The SPA bundle and the email templates are
    // compiled once and embedded into both binaries.
    golang({
      packageName: pkg.name,
      packagePath: './cmd',
      binArgs: ['serve'],
      build: {
        embedDir: 'web/output',
        devTarget: 'debug',
        targets: {
          debug: {
            outputDir: 'build/debug',
            buildTags: ['debug', 'noasm', 'nounsafe'],
            ldflags: goVersionLdflags
          },
          release: {
            outputDir: 'build/release',
            buildTags: ['release', 'noasm', 'nounsafe'],
            buildFlags: ['-trimpath', '-buildmode=pie', '-buildvcs=false'],
            ldflags: [...goVersionLdflags, '-w -s -extldflags -static']
          }
        }
      }
    })
  ],
  resolve: { tsconfigPaths: true },
  worker: { plugins: () => [comlink()] },
  build: {
    emptyOutDir: true,
    chunkSizeWarningLimit: 1024 * 4,
    outDir: resolve('web/output'),
    reportCompressedSize: false
  },
  server: isStorybook ? undefined : { port: 3000, strictPort: true, proxy: viteProxy },
  preview: { proxy: viteProxy }
})
