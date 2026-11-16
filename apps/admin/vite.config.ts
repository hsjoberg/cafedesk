import { defineConfig } from "vite";
import solid from "vite-plugin-solid";

// The build goes into the server, which embeds it. In development, Vite
// serves the admin and passes the API to the server on :8080.
export default defineConfig({
  plugins: [solid()],
  build: {
    outDir: "../server/web/dist",
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      "/v1": "http://localhost:8080",
    },
  },
});
