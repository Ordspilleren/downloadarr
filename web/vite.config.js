import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

// The build output goes to internal/webui/dist and is embedded into the Go
// binary. In dev, /api is proxied to the Go server.
export default defineConfig({
  plugins: [svelte()],
  build: {
    outDir: '../internal/webui/dist',
    emptyOutDir: true,
  },
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:8080',
    },
  },
})
