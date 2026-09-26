export type Theme = 'light' | 'dark'

// Preferencia por-viewer (no es sesion ni token): sin datos sensibles.
const THEME_STORAGE_KEY = 'argos-theme'

// Solo 'light' | 'dark' son validos; cualquier otra cosa (vacio, basura, storage bloqueado) -> claro.
export function readStoredTheme(): Theme {
  try {
    return window.localStorage.getItem(THEME_STORAGE_KEY) === 'dark' ? 'dark' : 'light'
  } catch {
    return 'light'
  }
}

export function storeTheme(theme: Theme): void {
  try {
    window.localStorage.setItem(THEME_STORAGE_KEY, theme)
  } catch {
    // Storage bloqueado o lleno: el tema sigue funcionando en memoria durante la sesion de la pestana.
  }
}

export function applyTheme(theme: Theme): void {
  document.documentElement.classList.toggle('dark', theme === 'dark')
}
