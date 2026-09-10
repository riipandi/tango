import path from "node:path";
import { defineConfig } from "vite";
import pkg from "./package.json" with { type: "json" };
import golang from "./plugins/plugin-golang.ts";

const isProduction = process.env.NODE_ENV === "production";
const BUILD_DATE = process.env.BUILD_DATE || new Date().toISOString();
const BUILD_HASH = process.env.BUILD_HASH || "dev";

export default defineConfig({
  plugins: [
    golang({
      packageName: pkg.name,
      packagePath: "./cmd",
      binArgs: ["serve"],
      build: {
        embedDir: "web/dist",
        outputDir: "build/release",
        buildTags: ["release"],
        buildFlags: ["-trimpath", "-a", "-buildmode=pie", "-buildvcs=false"],
        ldflags: [
          "-w -s -extldflags -static",
          `-X ${pkg.name}/internal/config.AppVersion=${pkg.version}`,
          `-X ${pkg.name}/internal/config.BuildHash=${BUILD_HASH}`,
          `-X ${pkg.name}/internal/config.BuildDate=${BUILD_DATE}`,
        ],
      },
    }),
  ],
  resolve: {
    alias: { "#": path.resolve("./app") },
    tsconfigPaths: true,
  },
  build: {
    emptyOutDir: true,
    chunkSizeWarningLimit: 1024 * 4,
    minify: isProduction ? "oxc" : false,
    reportCompressedSize: false,
    outDir: "web/dist",
  },
  server: {
    port: 3000,
    proxy: {
      "/.well-known": { target: "http://127.0.0.1:3080", changeOrigin: true },
      "/api": { target: "http://127.0.0.1:3080", changeOrigin: true },
      "/rpc": { target: "http://127.0.0.1:3080", changeOrigin: true },
      "/static": { target: "http://127.0.0.1:3080", changeOrigin: true },
    },
  },
});
