// Estado del sidebar (contraido/expandido). Preferencia por-viewer en localStorage, como el tema:
// todo acceso va en try/catch (storage bloqueado = la app funciona y el cambio dura hasta recargar).
export type SidebarState = 'collapsed' | 'expanded'

export const SIDEBAR_STORAGE_KEY = 'argos-sidebar'

/** Solo 'collapsed' y 'expanded' son validos; cualquier otro valor se ignora. */
export function parseSidebarState(raw: string | null): SidebarState | null {
  return raw === 'collapsed' || raw === 'expanded' ? raw : null
}

export function readStoredSidebar(): SidebarState | null {
  try {
    return parseSidebarState(localStorage.getItem(SIDEBAR_STORAGE_KEY))
  } catch {
    return null
  }
}

export function storeSidebar(state: SidebarState): void {
  try {
    localStorage.setItem(SIDEBAR_STORAGE_KEY, state)
  } catch {
    // storage no disponible: se sigue sin persistir
  }
}

/** Sin preferencia guardada: expandido desde `lg` (1024px), contraido entre `md` y `lg`. */
export function defaultSidebarState(isLarge: boolean): SidebarState {
  return isLarge ? 'expanded' : 'collapsed'
}
