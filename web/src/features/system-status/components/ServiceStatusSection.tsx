import type { ReactNode } from 'react'
import { Link } from 'react-router'
import { Cell, DataTable, HeadCell, HeadRow, Row, RowHeadCell, TableBody, TableHeader } from '@/components/data-table/DataTable'
import { InlineError } from '@/components/ui/inline-error'
import { Skeleton } from '@/components/ui/skeleton'
import { formatBytes } from '@/features/retention/utils/retention'
import { useSystemStatus } from '../hooks/useSystemStatus'
import { checkLabel, formatDuration } from '../utils/systemStatus'
import { LevelBadge } from './LevelBadge'

/** Sección "Estado del servicio" de Configuración (solo admin). */
export function ServiceStatusSection() {
  const { data, error, loading } = useSystemStatus()
  return (
    <section aria-labelledby="svc-title" aria-busy={loading} className="flex flex-col gap-3">
      <h2 id="svc-title" className="text-sm font-semibold">Estado del servicio</h2>
      {error && <InlineError>{error}</InlineError>}
      {loading && <Skeleton className="h-48 w-full" />}
      {data && (
        <>
          <dl className="grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
            <Item label="Estado"><LevelBadge level={data.status} /></Item>
            <Item label="Versión">{data.version}</Item>
            <Item label="Tiempo activo"><span className="tabular-nums">{formatDuration(data.uptime_seconds)}</span></Item>
            <Item label="Base de datos"><span className="tabular-nums">{formatBytes(data.database.size_bytes)}</span></Item>
          </dl>

          <DataTable caption="Chequeos del servicio" className="min-w-[36rem]">
            <TableHeader>
              <HeadRow>
                <HeadCell>Chequeo</HeadCell>
                <HeadCell>Estado</HeadCell>
                <HeadCell>Detalle</HeadCell>
              </HeadRow>
            </TableHeader>
            <TableBody>
              {data.checks.map((c) => (
                <Row key={c.name}>
                  <RowHeadCell>{checkLabel(c.name)}</RowHeadCell>
                  <Cell><LevelBadge level={c.status} /></Cell>
                  <Cell>{c.detail}</Cell>
                </Row>
              ))}
            </TableBody>
          </DataTable>

          {data.notifications.dead_24h.length > 0 && (
            <div className="flex flex-col gap-2">
              <h3 className="text-sm font-semibold">Entregas fallidas en 24 h</h3>
              <DataTable caption="Canales con entregas fallidas en 24 horas" className="min-w-[28rem]">
                <TableHeader>
                  <HeadRow>
                    <HeadCell>Canal</HeadCell>
                    <HeadCell>Dueño</HeadCell>
                    <HeadCell>Entregas fallidas</HeadCell>
                  </HeadRow>
                </TableHeader>
                <TableBody>
                  {data.notifications.dead_24h.map((d) => (
                    <Row key={d.channel_id}>
                      <RowHeadCell>{d.name}</RowHeadCell>
                      <Cell>{d.owner_group?.name ?? 'Global'}</Cell>
                      <Cell className="tabular-nums">{d.count}</Cell>
                    </Row>
                  ))}
                </TableBody>
              </DataTable>
              <Link to="/alertas/canales" className="text-sm text-brand underline">Ir a Canales</Link>
            </div>
          )}
        </>
      )}
    </section>
  )
}

function Item({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-0.5">
      <dt className="text-xs text-text-muted">{label}</dt>
      <dd>{children}</dd>
    </div>
  )
}
