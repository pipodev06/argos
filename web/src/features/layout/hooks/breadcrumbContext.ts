import { createContext, useContext, useEffect } from 'react'

/** Miga posterior al item del menu: `to` la vuelve enlace; la ultima siempre es la pagina actual. */
export interface Crumb {
  label: string
  to?: string
}

interface BreadcrumbState {
  setTrail: (trail: Crumb[] | null) => void
}

export const BreadcrumbContext = createContext<BreadcrumbState | null>(null)

/**
 * Las pantallas de detalle informan el nombre real de su entidad ("Hosts › laptop-local › Métricas"); sin trail
 * (cargando) las migas muestran "Detalle". Se limpia al desmontar.
 */
export function useBreadcrumbTrail(trail: Crumb[] | null) {
  const ctx = useContext(BreadcrumbContext)
  const key = trail ? JSON.stringify(trail) : null
  const setTrail = ctx?.setTrail
  useEffect(() => {
    if (!setTrail) return
    // key es la serializacion de trail: solo cambia si cambia su contenido.
    setTrail(key ? (JSON.parse(key) as Crumb[]) : null)
    return () => setTrail(null)
  }, [setTrail, key])
}
