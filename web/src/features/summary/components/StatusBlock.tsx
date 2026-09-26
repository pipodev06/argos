import { IconCircleCheck, IconCircleOff, IconCircleX, IconClock, IconPlugConnectedX } from '@tabler/icons-react'
import type { ReactNode } from 'react'
import { Link } from 'react-router'
import { Skeleton } from '@/components/ui/skeleton'
import { StatusTile, TILE_LINK } from '@/components/ui/status-tile'
import type { StatusCounts } from '../types/summary.types'

interface StatusBlockProps {
  /** Id del encabezado (aria-labelledby). */
  id: string
  title: string
  /** Sin datos todavia: se muestran esqueletos. */
  counts?: StatusCounts
  /** Ruta de la lista a la que enlaza cada tarjeta, con `?status=`. */
  to: string
  /** Texto `xs` discreto bajo el título (p. ej. agentes desactualizados). */
  note?: ReactNode
}

const TILES = [
  { label: 'Activos', status: 'up', Icon: IconCircleCheck, icon: 'text-ok' },
  { label: 'Caídos', status: 'down', Icon: IconCircleX, icon: 'text-danger' },
  { label: 'Sin conexión', status: 'offline', Icon: IconPlugConnectedX, icon: 'text-text-muted' },
  { label: 'Pendientes', status: 'pending', Icon: IconClock, icon: 'text-info' },
  { label: 'Deshabilitados', status: 'disabled', Icon: IconCircleOff, icon: 'text-text-muted' },
] as const

/** Bloque de 3 filas x 2 columnas (Activos/Caídos, Sin conexión/Pendientes, Deshabilitados a ancho completo). */
export function StatusBlock({ id, title, counts, to, note }: StatusBlockProps) {
  return (
    <section aria-labelledby={id} className="flex flex-col gap-3">
      <h2 id={id} className="text-sm font-semibold">
        {title}
      </h2>
      {note && <p className="-mt-2 text-xs text-text-muted">{note}</p>}
      <div className="grid grid-cols-2 gap-4">
        {TILES.map((t) => {
          const span = t.status === 'disabled' ? ' col-span-2' : ''
          return counts ? (
            <Link key={t.status} to={`${to}?status=${t.status}`} className={TILE_LINK + span}>
              <StatusTile
                label={t.label}
                value={counts[t.status]}
                Icon={t.Icon}
                iconClassName={t.icon}
                alert={t.status === 'down' && counts.down > 0}
              />
            </Link>
          ) : (
            <Skeleton key={t.status} className={'h-24' + span} />
          )
        })}
      </div>
    </section>
  )
}
