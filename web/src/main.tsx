import ReactDOM from 'react-dom/client'
import App from './App.tsx'
import { ThemeProvider } from './theme/ThemeProvider'
import { I18nProvider } from './i18n'
import './index.css'

// A tab kept open during a deployment can still reference an old hashed
// route chunk. Refresh once to fetch the new HTML; if that same refresh also
// fails, let the route error boundary show a visible recovery action instead
// of entering a reload loop.
const CHUNK_RELOAD_KEY = 'celikpanel.chunk-reload-at'
const CHUNK_RELOAD_WINDOW_MS = 30_000

window.addEventListener('vite:preloadError', (event) => {
  try {
    const lastReload = Number(window.sessionStorage.getItem(CHUNK_RELOAD_KEY) ?? '0')
    const now = Date.now()
    if (!Number.isFinite(lastReload) || now - lastReload > CHUNK_RELOAD_WINDOW_MS) {
      window.sessionStorage.setItem(CHUNK_RELOAD_KEY, String(now))
      event.preventDefault()
      window.location.reload()
    }
  } catch {
    // Storage can be unavailable in hardened browser modes. In that case the
    // rejected lazy import continues to the route error boundary.
  }
})

ReactDOM.createRoot(document.getElementById('root')!).render(
  <ThemeProvider>
    <I18nProvider>
      <App />
    </I18nProvider>
  </ThemeProvider>
)
