/// <reference types="vitest/config" />
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
  build: {
    rolldownOptions: {
      output: {
        // Vite 8 (rolldown): vendors estables en chunks propios (cachean entre despliegues). Recharts queda solo
        // en el chunk de las gráficas: nada de gráficas se carga hasta abrir una pantalla que las use.
        codeSplitting: {
          groups: [
            { name: 'vendor-react', test: /node_modules[/](react|react-dom|scheduler)[/]/, priority: 30 },
            { name: 'vendor-router', test: /node_modules[/]react-router[/]/, priority: 20 },
            { name: 'vendor-radix', test: /node_modules[/](@radix-ui|radix-ui|@floating-ui)[/]/, priority: 10 },
          ],
        },
      },
    },
  },
  // entorno node por defecto; los tests de hooks piden jsdom con `// @vitest-environment jsdom`
  test: { globals: false, setupFiles: ['./src/test/setup.ts'] },
  resolve: {
    alias: {
      '@': path.resolve(import.meta.dirname, './src'),
    },
  },
})
