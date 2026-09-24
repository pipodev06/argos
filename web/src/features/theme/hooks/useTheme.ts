import { useSyncExternalStore } from 'react'
import { applyTheme, readStoredTheme, storeTheme, type Theme } from '../utils/theme'

// Store de modulo (sin Provider): el tema no depende de la sesion y lo leen login, dashboard y dialogs.
let current: Theme = 'light'
const listeners = new Set<() => void>()

function setTheme(theme: Theme) {
  current = theme
  applyTheme(theme)
  storeTheme(theme)
  listeners.forEach((listener) => listener())
}

function subscribe(listener: () => void) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

// Llamar en main.tsx antes del primer render: evita parpadeo del tema equivocado.
export function initTheme(): void {
  current = readStoredTheme()
  applyTheme(current)
}

export function useTheme() {
  const theme = useSyncExternalStore(subscribe, () => current)
  return {
    theme,
    toggleTheme: () => setTheme(theme === 'dark' ? 'light' : 'dark'),
  }
}
