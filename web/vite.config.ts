import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'

// https://vitejs.dev/config/
export default defineConfig({
  plugins: [react()],
  build: {
    // The post-build budget follows this graph so a route cannot hide a large
    // payload behind several individually small shared chunks.
    manifest: true,
  },
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  experimental: {
    // CSS-referenced assets (fonts) resolve relative to the CSS file, not the
    // site root — the stylesheet then works wherever it is served from.
    // / CSS içinden başvurulan varlıklar (fontlar) site köküne değil CSS
    // dosyasına göre çözülür — stylesheet nereden sunulursa sunulsun çalışır.
    renderBuiltUrl(_filename, { hostType }) {
      if (hostType === 'css') return { relative: true }
    },
  },
})
