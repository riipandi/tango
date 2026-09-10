import * as fs from "node:fs";
import * as path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { render, pretty } from "react-email";

// Get the directory of this script file
const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);

// Resolve paths relative to script location
const templatesDir = path.resolve(__dirname, "templates");
const outputDir = path.resolve(process.cwd(), "web/email");

// Log style kept in sync with plugins/plugin-golang.ts
const C = {
  reset: "\x1b[0m",
  dim: "\x1b[2m",
  green: "\x1b[32m",
  red: "\x1b[31m",
  cyan: "\x1b[36m",
} as const;

const PREFIX = `${C.cyan}[email]${C.reset}`;

function log(msg: string) {
  console.log(`${PREFIX} ${msg}`);
}

function logInfo(label: string, value: string) {
  console.log(`${PREFIX} ${C.dim}${label.padEnd(10)}${C.reset}${value}`);
}

function logError(msg: string) {
  console.error(`${PREFIX} ${C.red}${msg}${C.reset}`);
}

function formatDuration(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

function getTemplateName(filename: string): string {
  return filename.replace(".tsx", "");
}

function getFirstExport(module: Record<string, unknown>): unknown {
  const keys = Object.keys(module);
  if (keys.length === 0) return undefined;
  const firstKey = keys[0];
  if (firstKey === undefined) return undefined;
  return module[firstKey];
}

async function buildTemplateFile(Component: any, templateName: string, isPlainText: boolean) {
  const isProd = process.env.APP_MODE === "production" || process.env.NODE_ENV === "production";

  // `plainText` is a discriminated union, so it must be a literal, not a boolean.
  const element = Component(Component.TemplateProps);
  let rendered = isPlainText
    ? await render(element, { plainText: true })
    : await render(element, { plainText: false });

  // Pretty-print HTML in development, keep minified in production
  // (plain text is never pretty-printed)
  if (!isPlainText && !isProd) {
    rendered = await pretty(rendered);
  }

  // Normalize quotes
  const normalized = rendered.replace(/&quot;/g, '"');

  const goTemplate = `{{define "root"}}${normalized}{{end}}`;
  const suffix = isPlainText ? "_text.tmpl" : "_html.tmpl";
  const templatePath = path.join(outputDir, `${templateName}${suffix}`);

  fs.writeFileSync(templatePath, goTemplate);
}

async function discoverAndBuildTemplates() {
  const templatesLabel = path.relative(process.cwd(), templatesDir);
  const outputLabel = path.relative(process.cwd(), outputDir);

  log("building email templates...");
  logInfo("templates", templatesLabel);
  logInfo("output", outputLabel);

  if (!fs.existsSync(templatesDir)) {
    logError(`templates directory not found: ${templatesLabel}`);
    process.exitCode = 1;
    return;
  }

  if (!fs.existsSync(outputDir)) {
    fs.mkdirSync(outputDir, { recursive: true });
  }

  const files = fs.readdirSync(templatesDir);
  const startedAt = Date.now();
  let built = 0;
  let failed = 0;

  for (const file of files) {
    if (!file.endsWith(".tsx")) continue;

    const templateName = getTemplateName(file);
    const filePath = path.join(templatesDir, file);
    const fileUrl = pathToFileURL(filePath).href;
    const start = Date.now();

    try {
      const importedModule = await import(fileUrl);
      const Component = importedModule.default ?? getFirstExport(importedModule);

      if (!Component) {
        throw new Error("no component export found");
      }

      if (!Component.TemplateProps) {
        throw new Error("no TemplateProps export found");
      }

      await buildTemplateFile(Component, templateName, false); // HTML
      await buildTemplateFile(Component, templateName, true); // Text

      built++;
      log(`  ${C.green}✓ ${templateName}${C.reset} in ${formatDuration(Date.now() - start)}`);
    } catch (error) {
      failed++;
      const message = error instanceof Error ? error.message : String(error);
      logError(`  ✗ ${templateName}: ${message}`);
    }
  }

  const total = built + failed;
  const duration = formatDuration(Date.now() - startedAt);

  if (built > 0) {
    log(`${C.green}built ${built}/${total} templates → ${outputLabel} in ${duration}${C.reset}\n`);
  } else if (total === 0) {
    log(`no templates found in ${templatesLabel}\n`);
  }

  if (failed > 0) {
    logError(`${failed} of ${total} template(s) failed`);
    process.exitCode = 1;
  }
}

discoverAndBuildTemplates().catch((error) => {
  logError(error instanceof Error ? error.message : String(error));
  process.exitCode = 1;
});
