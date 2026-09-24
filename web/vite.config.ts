import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    // el server Go no tiene CORS: en dev todo /api se proxea a el
    // changeOrigin false: el Host debe seguir siendo el del navegador pa que el CSRF del server (Origin == Host) pase
    proxy: { '/api': { target: 'http://localhost:8080', changeOrigin: false } },
  },
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, './src'),
    },
  },
})
