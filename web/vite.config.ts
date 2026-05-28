import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

export default defineConfig({
  plugins: [react()],
  build: {
    outDir: '../internal/interface/api/k8s-dist',
    emptyOutDir: true,
    rollupOptions: {
      input: path.resolve(__dirname, 'index.html'),
    },
  },
  server: {
    proxy: {
      '/branches': 'http://localhost:8080',
      '/snapshots': 'http://localhost:8080',
      '/stats': 'http://localhost:8080',
      '/health': 'http://localhost:8080',
    },
  },
})
