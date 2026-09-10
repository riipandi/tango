import { spawn, type ChildProcess } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import type { Plugin, ViteDevServer } from "vite";

export interface GoBuildOptions {
  args?: string[];
  packagePath?: string;
  outputDir?: string;
  outputBin?: string;
  embedDir?: string;
  buildTags?: string[];
  buildFlags?: string[];
  ldflags?: string[];
}

export interface PluginGolangOptions {
  packageName: string;
  cmd?: string;
  args?: string[];
  packagePath?: string;
  bin?: string;
  binArgs?: string[];
  delay?: number;
  killDelay?: number;
  stopOnError?: boolean;
  excludeDir?: string[];
  excludeRegex?: string[];
  extensions?: string[];
  log?: boolean;
  build?: GoBuildOptions;
}

interface ResolvedBuildOptions {
  outputDir: string;
  outputBin: string;
  embedDir: string;
  args: string[];
  buildTags: string[];
  buildFlags: string[];
  ldflags: string[];
}

interface GoPluginDefaults {
  cmd: string;
  binArgs: string[];
  delay: number;
  killDelay: number;
  stopOnError: boolean;
  excludeDir: string[];
  excludeRegex: string[];
  extensions: string[];
  log: boolean;
  build: Required<GoBuildOptions>;
}

const defaults: GoPluginDefaults = {
  cmd: "go",
  binArgs: [],
  delay: 1000,
  killDelay: 300,
  stopOnError: false,
  excludeDir: [".git", "vendor", "node_modules", "web/dist", "temp", "tmp", "build", "dist"],
  excludeRegex: ["_test\\.go$"],
  extensions: ["go"],
  log: true,
  build: {
    args: [],
    packagePath: ".",
    outputDir: "",
    outputBin: "",
    embedDir: "web/dist",
    buildTags: [],
    buildFlags: [],
    ldflags: [],
  },
};

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

function formatFileSize(bytes: number): string {
  const mb = (bytes / (1024 * 1024)).toFixed(2);
  const kb = (bytes / (1024 * 1024)).toFixed(1);
  return bytes >= 1024 * 1024 ? `${mb} MB` : `${kb} KB`;
}

function formatBuildInfo(buildOpts: ResolvedBuildOptions): string[] {
  const isProduction = process.env.NODE_ENV === "production";
  const mode = isProduction ? "release" : "debug";
  const tags = buildOpts.buildTags.length > 0 ? buildOpts.buildTags.join(", ") : "none";

  const lines: string[] = [];
  lines.push(`  mode     ${mode}`);
  lines.push(`  tags     ${tags}`);

  if (buildOpts.buildFlags.length > 0) {
    lines.push(`  flags    ${buildOpts.buildFlags.join(" ")}`);
  }

  if (buildOpts.ldflags.length > 0) {
    lines.push(`  ldflags  ${buildOpts.ldflags.join(" ")}`);
  }

  lines.push(`  embed    ${buildOpts.embedDir}`);
  lines.push(`  output   ${buildOpts.outputDir}/${buildOpts.outputBin}`);

  return lines;
}

function resolveBuildOptions(
  userBuild: GoBuildOptions | undefined,
  userPkg: string,
  packageName: string,
): ResolvedBuildOptions {
  const isProduction = process.env.NODE_ENV === "production";

  const outputDir = userBuild?.outputDir || (isProduction ? "build/release" : "build/debug");
  const outputBin = userBuild?.outputBin || packageName;
  const embedDir = userBuild?.embedDir || "web/dist";
  const buildFlags = userBuild?.buildFlags || [];
  const ldflags = userBuild?.ldflags || [];
  const buildTags = userBuild?.buildTags || defaults.build.buildTags;
  const pkg = userBuild?.packagePath || userPkg;

  const buildArgs =
    userBuild?.args && userBuild.args.length > 0
      ? [...userBuild.args]
      : ["build", "-o", `${outputDir}/${outputBin}`, pkg];

  if (buildTags.length > 0) {
    buildArgs.splice(1, 0, "-tags", buildTags.join(","));
  }

  if (buildFlags.length > 0) {
    buildArgs.splice(1, 0, ...buildFlags);
  }

  if (ldflags.length > 0) {
    buildArgs.splice(1, 0, "-ldflags", ldflags.join(" "));
  }

  return { outputDir, outputBin, embedDir, args: buildArgs, buildFlags, ldflags, buildTags };
}

export default function VitePlugin(userOptions: PluginGolangOptions): Plugin {
  if (process.env.VITEST || process.env.STORYBOOK) {
    return { name: "vite-plugin-go" };
  }

  const name = userOptions.packageName;
  const pkgPath = userOptions.packagePath || defaults.build.packagePath;
  const defaultBin = `build/debug/${name}`;
  const defaultArgs = ["build", "-o", defaultBin, pkgPath];

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
    build: { ...defaults.build, ...userOptions.build },
  };

  const excludePatterns = [
    ...opts.excludeDir.map((d) => new RegExp(`[\\/]${path.normalize(d)}[\\/]`)),
    ...opts.excludeRegex.map((r) => new RegExp(r)),
  ];

  let goProcess: ChildProcess | null = null;
  let buildTimer: ReturnType<typeof setTimeout> | null = null;
  let isBuilding = false;
  let hasPendingChanges = false;
  let disposed = false;

  const buildOpts = resolveBuildOptions(userOptions.build, pkgPath, name);

  function log(msg: string) {
    if (opts.log) console.log(`\x1b[36m[go]\x1b[0m ${msg}`);
  }

  function killGo() {
    if (!goProcess) return;
    const proc = goProcess;
    goProcess = null;
    try {
      process.kill(-proc.pid!, "SIGTERM");
    } catch {
      proc.kill("SIGTERM");
    }
  }

  function dispose() {
    if (disposed) return;
    disposed = true;
    if (buildTimer) clearTimeout(buildTimer);
    killGo();
    log("stopped");
  }

  function startBinary() {
    const proc = spawn(opts.bin, opts.binArgs, {
      stdio: "inherit",
      detached: true,
    });
    goProcess = proc;

    proc.on("error", (err) => {
      log(`failed to start binary: ${err.message}`);
      goProcess = null;
    });
  }

  function runBuild(
    onSuccess: () => void,
    onFailure: (code: number | null, stderr: string) => void,
  ) {
    isBuilding = true;
    log("building...");

    const buildProcess = spawn(opts.cmd, opts.args, { stdio: "pipe" });
    let stderr = "";

    buildProcess.stderr?.on("data", (data: Buffer) => {
      stderr += data.toString();
    });

    buildProcess.on("close", (code) => {
      isBuilding = false;

      if (code === 0) {
        log("built successfully");
        onSuccess();
      } else {
        log(`build failed (exit code ${code})`);
        if (stderr) console.error(stderr);
        onFailure(code, stderr);
      }
    });
  }

  function buildAndStart() {
    if (isBuilding || disposed) return;
    if (buildTimer) clearTimeout(buildTimer);

    buildTimer = setTimeout(() => {
      runBuild(
        () => {
          buildTimer = null;

          if (hasPendingChanges) {
            hasPendingChanges = false;
            buildAndStart();
            return;
          }

          setTimeout(() => {
            if (disposed) return;
            killGo();
            startBinary();
          }, opts.killDelay);
        },
        (_code, _stderr) => {
          buildTimer = null;
          if (opts.stopOnError) killGo();

          if (hasPendingChanges) {
            hasPendingChanges = false;
            buildAndStart();
          }
        },
      );
    }, opts.delay);
  }

  function initialBuild() {
    runBuild(
      () => {
        if (disposed) return;
        startBinary();
      },
      () => {},
    );
  }

  function shouldWatch(filePath: string): boolean {
    const ext = path.extname(filePath).slice(1);
    if (!opts.extensions.includes(ext)) return false;
    if (excludePatterns.some((re) => re.test(filePath))) return false;
    return true;
  }

  return {
    name: "vite-plugin-go",
    configureServer(_server: ViteDevServer) {
      initialBuild();

      _server.watcher.on("change", (file: string) => {
        if (disposed) return;
        if (!shouldWatch(file)) return;

        if (isBuilding) {
          hasPendingChanges = true;
          return;
        }

        buildAndStart();
      });

      const onSignal = () => {
        dispose();
        process.exit(0);
      };
      process.on("SIGINT", onSignal);
      process.on("SIGTERM", onSignal);
    },
    closeBundle: {
      sequential: true,
      order: "post",
      handler() {
        const embedDir = buildOpts.embedDir;
        if (!fs.existsSync(embedDir)) {
          log(`embed directory "${embedDir}" not found, skipping go build`);
          return;
        }

        fs.mkdirSync(buildOpts.outputDir, { recursive: true });

        log("building binary...");
        for (const line of formatBuildInfo(buildOpts)) {
          log(line);
        }

        const startTime = Date.now();
        const buildProcess = spawn(opts.cmd, buildOpts.args, { stdio: "pipe" });
        let stderr = "";

        buildProcess.stderr?.on("data", (data: Buffer) => {
          stderr += data.toString();
        });

        return new Promise<void>((resolve) => {
          buildProcess.on("close", (code) => {
            const duration = Date.now() - startTime;

            if (code === 0) {
              const binPath = `${buildOpts.outputDir}/${buildOpts.outputBin}`;
              const stat = fs.statSync(binPath);
              log(
                `binary built → ${binPath} (${formatFileSize(stat.size)}) in ${formatDuration(duration)}`,
              );
            } else {
              log(`build failed (exit code ${code}) in ${formatDuration(duration)}`);
              if (stderr) console.error(stderr);
              process.exitCode = 1;
            }
            resolve();
          });
        });
      },
    },
  };
}
