import type { ReactNode } from 'react'
import { LineSwatch } from '@/components/ui/chart'
import type { SeriesStyle } from '../utils/chart'

export type ChartStatus = 'loading' | 'empty' | 'error' | 'ready'

interface MetricChartCardProps {
  title: string
  /** Leyenda directa: una entrada por serie (y por linea de referencia), con su patron de trazo. */
  legend: readonly SeriesStyle[]
  status: ChartStatus
  errorMessage?: string | null
  /** Alternativa textual de la grafica (role="img"). */
  description: string
  children: ReactNode
}

const MESSAGES: Record<Exclude<ChartStatus, 'ready' | 'error'>, string> = {
  loading: 'Cargando…',
  empty: 'Sin datos en este rango.',
}

/** Contenedor de grafica: titulo, leyenda y cuerpo de altura fija (sin saltos de layout entre estados). */
export function MetricChartCard({
  title,
  legend,
  status,
  errorMessage,
  description,
  children,
}: MetricChartCardProps) {
  return (
    <section className="rounded-md border border-border bg-surface p-4">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-x-4 gap-y-1">
        <h3 className="text-sm font-semibold">{title}</h3>
        <ul className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-text-muted">
          {legend.map((item) => (
            <li key={item.name} className="flex items-center gap-1.5">
              <LineSwatch color={item.color} dash={item.dash} />
              {item.name}
            </li>
          ))}
        </ul>
      </div>
      <div className="h-56">
        {status === 'ready' ? (
          <div role="img" aria-label={description} className="h-full">
            {children}
          </div>
        ) : (
          <div className="flex h-full items-center justify-center text-sm text-text-muted">
            {status === 'error' ? (errorMessage ?? 'No se pudieron cargar las métricas.') : MESSAGES[status]}
          </div>
        )}
      </div>
    </section>
  )
}
