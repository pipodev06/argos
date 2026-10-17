import { useCallback, useState } from 'react'
import { defaultSidebarState, readStoredSidebar, storeSidebar } from '../utils/sidebar'

function initialCollapsed(): boolean {
  const stored = readStoredSidebar()
  if (stored) return stored === 'collapsed'
  return defaultSidebarState(window.matchMedia('(min-width: 1024px)').matches) === 'collapsed'
}

/** Sidebar de escritorio contraido o no; se recuerda al alternarlo (no antes: el valor por defecto no se guarda). */
export function useSidebarCollapsed() {
  const [collapsed, setCollapsed] = useState(initialCollapsed)

  const set = useCallback((next: boolean) => {
    setCollapsed(next)
    storeSidebar(next ? 'collapsed' : 'expanded')
  }, [])

  return { collapsed, setCollapsed: set }
}
