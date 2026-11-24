import path from 'node:path'
import tailwindcss from '@tailwindcss/vite'
import react from '@vitejs/plugin-react'
import { defineConfig } from 'vite'

// ビルド結果は Go のバイナリに埋め込む（internal/webapp/dist）。ページの URL にはどれも index.html が返る。
// 開発中は vite が /api やファイルのダウンロードを Go のサーバー（mise run dev, :8080）に中継する。
export default defineConfig({
  base: '/',
  plugins: [react(), tailwindcss()],
  resolve: { alias: { '@': path.resolve(import.meta.dirname, './src') } },
  build: { outDir: '../internal/webapp/dist', emptyOutDir: true },
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      '/download': 'http://localhost:8080',
      '/downloads/': 'http://localhost:8080',
      '^/courses/[^/]+/(files\\.json|files\\.zip|summary)': 'http://localhost:8080',
    },
  },
})
