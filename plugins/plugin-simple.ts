import type { Plugin, ViteDevServer } from "vite";

export default function VitePlugin(): Plugin {
  if (process.env.VITEST || process.env.STORYBOOK) {
    return { name: "vite-plugin-go" };
  }

  return {
    name: "vite-plugin-simple",
    configureServer(_server: ViteDevServer) {
      console.log("Not yet implemented");
    },
  };
}
