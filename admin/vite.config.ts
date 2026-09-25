import { defineConfig } from "vite";
import preact from "@preact/preset-vite";
import tailwindcss from "@tailwindcss/vite";

// The admin SPA is served at /_/ by both runtimes, so all asset URLs are
// rooted there. The build is emitted into the Go runtime's package tree so it
// can be go:embed-ed; a sync step mirrors it into the edge runtime.
export default defineConfig({
  base: "/_/",
  plugins: [preact(), tailwindcss()],
  // `npm run admin:dev` talks to a runtime on :3000 (`npm run serve`).
  server: {
    proxy: { "/_/api": "http://localhost:3000" },
  },
  build: {
    outDir: "../runtime/go/admin/spa",
    emptyOutDir: true,
  },
});
