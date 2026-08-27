import { writeFileSync } from "node:fs";
import { join } from "node:path";
import { defineConfig, type Plugin } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import tailwindcss from "@tailwindcss/vite";
import { paraglideVitePlugin } from "@inlang/paraglide-js";
import wails from "@wailsio/runtime/plugins/vite";

// dist/.gitkeep is committed so the Go embed compiles before the first build;
// recreate it after vite empties the output directory
function keepGitkeep(): Plugin {
  return {
    name: "keep-gitkeep",
    closeBundle() {
      writeFileSync(join(import.meta.dirname, "dist", ".gitkeep"), "");
    },
  };
}

// https://vitejs.dev/config/
export default defineConfig({
  server: {
    host: "127.0.0.1",
    port: Number(process.env.WAILS_VITE_PORT) || 9245,
    strictPort: true,
  },
  plugins: [
    tailwindcss(),
    svelte(),
    paraglideVitePlugin({
      project: "./project.inlang",
      outdir: "./src/paraglide",
      // The Go host sets the locale once at startup from appInfo.resolvedLanguage
      // (see main.ts); there's no SSR or in-app reload, so an in-memory locale is
      // all we need. baseLocale (en) is the fallback before setLocale runs.
      strategy: ["globalVariable", "baseLocale"],
    }),
    wails("./bindings"),
    keepGitkeep(),
  ],
});
