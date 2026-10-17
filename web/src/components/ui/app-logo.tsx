import { cn } from 'cn'

/**
 * Marca de argos: ojo (el vigilante de cien ojos) atravesado por una linea de pulso (monitoreo), en un cuadro
 * invertido (`bg-text`/`text-bg`, neutro: nunca brand ni color de estado). Mismo dibujo que `public/favicon.svg`.
 */
function AppLogoMark({ className }: { className?: string }) {
  return (
    <span className={cn('grid size-7 shrink-0 place-items-center rounded-md bg-text text-bg', className)} aria-hidden>
      <svg viewBox="0 0 24 24" className="size-5" fill="none" stroke="currentColor" strokeWidth={1.75} strokeLinecap="round" strokeLinejoin="round">
        <path d="M2 12S5.5 5 12 5s10 7 10 7-3.5 7-10 7S2 12 2 12Z" />
        <path d="M5.5 12h3l1.5-3 3 6 1.5-3h4" />
      </svg>
    </span>
  )
}

/** Logo completo: marca + nombre. `withName=false` deja solo la marca (sidebar contraido). */
function AppLogo({ withName = true, className }: { withName?: boolean; className?: string }) {
  return (
    <span className={cn('inline-flex items-center gap-2', className)}>
      <AppLogoMark />
      {withName && <span className="text-lg font-semibold tracking-tight">argos</span>}
    </span>
  )
}

export { AppLogo, AppLogoMark }
