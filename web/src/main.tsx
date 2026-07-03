import ReactDOM from 'react-dom/client'
import App from './App.tsx'
import { ThemeProvider } from './theme/ThemeProvider'
import { I18nProvider } from './i18n'
import './index.css'

ReactDOM.createRoot(document.getElementById('root')!).render(
  <ThemeProvider>
    <I18nProvider>
      <App />
    </I18nProvider>
  </ThemeProvider>
)
