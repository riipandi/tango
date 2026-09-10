import { defineConfig, type ProxyOptions } from "vite";
import pkg from "./package.json" with { type: "json" };
import golang from "./plugins/plugin-golang.ts";
import { resolve } from "node:path";

// Must match the module path in go.mod.
const goModule = "github.com/riipandi/tango";

// const isProduction = process.env.NODE_ENV === "production";
// const isTestOrCI = process.env.CI || process.env.VITEST
// const isVitest = process.env.VITEST
const isStorybook = process.env.STORYBOOK === "true";
const BUILD_DATE = process.env.BUILD_DATE || new Date().toISOString();
const BUILD_HASH = process.env.BUILD_HASH || "dev";

// Same-origin proxy to the demo auth backend. Required for the HttpOnly
// cookie session: cookies default to SameSite=Lax, which is not sent on
// cross-site fetches, and this keeps them first-party.
const viteProxy: Record<string, string | ProxyOptions> = {
  "/.well-known": { target: "http://127.0.0.1:3080", changeOrigin: true },
  "/static": { target: "http://127.0.0.1:3080", changeOrigin: true },
  "/api": { target: "http://127.0.0.1:3080", changeOrigin: true },
};

export default defineConfig({
  plugins: [
    golang({
      packageName: pkg.name,
      packagePath: "./cmd",
      binArgs: ["serve"],
      build: {
        embedDir: "web/output",
        outputDir: "build/release",
        buildTags: ["release"],
        buildFlags: ["-trimpath", "-a", "-buildmode=pie", "-buildvcs=false"],
        ldflags: [
          "-w -s -extldflags -static",
          `-X ${goModule}/internal/config.AppVersion=${pkg.version}`,
          `-X ${goModule}/internal/config.BuildHash=${BUILD_HASH}`,
          `-X ${goModule}/internal/config.BuildDate=${BUILD_DATE}`,
        ],
      },
    }),
  ],
  resolve: { tsconfigPaths: true },
  build: {
    emptyOutDir: true,
    chunkSizeWarningLimit: 1024 * 4,
    outDir: resolve("web/output"),
    reportCompressedSize: false,
  },
  server: isStorybook ? undefined : { port: 3000, strictPort: true, proxy: viteProxy },
  preview: { proxy: viteProxy },
});
