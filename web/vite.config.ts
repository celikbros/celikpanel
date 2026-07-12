import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import path from 'path'
import { execSync } from 'child_process'

// Stamp the build with the short git commit at build time so the running UI
// can show exactly which revision it is — a hard-refresh then tells the
// operator at a glance whether a deploy actually landed. Falls back to "dev".
// Derlemeyi build anındaki kısa git commit'iyle damgala; böylece çalışan
// arayüz tam olarak hangi revizyonda olduğunu gösterir — sert yenileme,
// operatöre bir dağıtımın gerçekten inip inmediğini tek bakışta söyler.
const buildStamp = (() => {
  try {
    return execSync('git rev-parse --short HEAD').toString().trim()
  } catch {
    return 'dev'
  }
})()

// https://vitejs.dev/config/
export default defineConfig({
  define: {
    __BUILD__: JSON.stringify(buildStamp),
  },
  plugins: [react()],
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
