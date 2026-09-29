import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'
import { getConfig } from './lib/api.ts'
import { applyLanguagePref, initI18n } from './i18n'
import { initTheme } from './lib/theme'

// Before render: sync theme-color/Wails to the pre-paint class and start
// following OS and cross-tab theme changes.
initTheme()
initI18n()
// A stored ui_language pref (if any) applies at startup; without a
// backend the resolved system language stays in effect.
getConfig().then((c) => applyLanguagePref(c.ui_language)).catch(() => {})

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
