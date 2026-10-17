import { useCallback, useState } from 'react'
import { OPTIONAL_COLUMNS, type OptionalColumn } from '../utils/columns'

const STORAGE_KEY = 'argos.hosts.columns'

function load(): OptionalColumn[] {
  try {
    const raw: unknown = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? '[]')
    if (!Array.isArray(raw)) return []
    return OPTIONAL_COLUMNS.map((c) => c.id).filter((id) => raw.includes(id))
  } catch {
    return []
  }
}

/** Columnas opcionales visibles (persistidas en localStorage; sin acceso, solo en memoria). */
export function useColumnVisibility() {
  const [visible, setVisible] = useState<OptionalColumn[]>(load)

  const toggle = useCallback(
    (id: OptionalColumn, on: boolean) => {
      const next = OPTIONAL_COLUMNS.map((c) => c.id).filter((c) => (c === id ? on : visible.includes(c)))
      setVisible(next)
      try {
        localStorage.setItem(STORAGE_KEY, JSON.stringify(next))
      } catch {
        // almacenamiento no disponible: la preferencia vive solo esta sesion
      }
    },
    [visible],
  )

  return { visible, toggle }
}
