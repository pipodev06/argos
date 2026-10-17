import { cn } from 'cn'

/** Marca de argos (ojo vigilante en un cuadro invertido) + nombre opcional. Mismo dibujo que `public/favicon.svg`. */
function AppLogo({ withName = true, className }: { withName?: boolean; className?: string }) {
  return (
    <span className={cn('inline-flex items-center gap-2', className)}>
      <span className="grid size-6 shrink-0 place-items-center rounded-md bg-text text-bg" aria-hidden>
        <svg viewBox="0 0 24 24" className="size-4" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round">
          <path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12Z" />
          <circle cx="12" cy="12" r="3.25" />
        </svg>
      </span>
      {withName && <span className="text-sm font-semibold">argos</span>}
    </span>
  )
}

export { AppLogo }
