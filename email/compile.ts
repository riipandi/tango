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

// Create output directory if it doesn't exist
if (!fs.existsSync(outputDir)) {
  fs.mkdirSync(outputDir, { recursive: true });
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

  const element = Component(Component.TemplateProps);

  // `plainText` is a discriminated union, so it must be a literal, not a boolean.
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
  console.log("Discovering and building email templates...");
  console.log("Templates directory:", templatesDir);
  console.log("Output directory:", outputDir, "\n");

  if (!fs.existsSync(templatesDir)) {
    console.error(`✗ Templates directory not found: ${templatesDir}`);
    return;
  }

  const files = fs.readdirSync(templatesDir);

  for (const file of files) {
    if (!file.endsWith(".tsx")) continue;

    const templateName = getTemplateName(file);
    const filePath = path.join(templatesDir, file);
    const fileUrl = pathToFileURL(filePath).href;

    console.log(`- Building ${templateName}...`);

    try {
      const importedModule = await import(fileUrl);
      const Component = importedModule.default ?? getFirstExport(importedModule);

      if (!Component) {
        console.error(`✗ No component found in ${file}`);
        continue;
      }

      if (!Component.TemplateProps) {
        console.error(`✗ No TemplateProps found in ${file}`);
        continue;
      }

      await buildTemplateFile(Component, templateName, false); // HTML
      await buildTemplateFile(Component, templateName, true); // Text

      console.log(`✓ Built ${templateName}`);
    } catch (error) {
      console.error(`✗ Error building ${templateName}:`, error);
    }
  }
}

async function main() {
  await discoverAndBuildTemplates();
  console.log("\nAll email templates built successfully!");
}

main().catch(console.error);
