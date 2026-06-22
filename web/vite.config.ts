import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

// Single-page app served by the Go backend in production. During development
// `npm run dev` proxies API + SSE calls to the backend on :8080.
export default defineConfig({
  plugins: [react()],
  base: '/',
  build: { outDir: 'dist', emptyOutDir: true },
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://localhost:8080', changeOrigin: true }
    }
  }
})
