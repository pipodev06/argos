import { Fragment, useCallback, useState } from 'react'
import { ArrowRightIcon, ChevronDownIcon, ActivityIcon } from 'lucide-react'
import { Cell, DataTable, HeadCell, HeadRow, Row, RowNumberCell, RowNumberHeadCell, TableBody, TableHeader } from '@/components/data-table/DataTable'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'
import { InlineError } from '@/components/ui/inline-error'
import { Skeleton } from '@/components/ui/skeleton'
import { formatSeconds } from '@/features/alerts/utils/format'
import { formatElapsed, formatFullDate } from '@/features/hosts/utils/format'
import { usePolledFetch } from '@/hooks/usePolledFetch'
import { useNow } from '@/hooks/useNow'
import { useHostChecks } from '../hooks/useChecks'
import { listCheckEvents } from '../services/checks.service'
import type { CheckEvent, HostCheck } from '../types/check.types'
import { CHECK_TYPE_LABELS, detailLabel, hostCheckValue } from '../utils/format'
import { CheckStatusBadge } from './CheckStatusBadge'

function EventsPanel({ hostId, checkId }: { hostId: string; checkId: string }) {
  // Primera página al montar (sin refresco); "Cargar más" agrega páginas siguientes por cursor.
  const fetcher = useCallback((signal: AbortSignal) => listCheckEvents(hostId, checkId, null, signal), [hostId, checkId])
  const first = usePolledFetch(fetcher, { intervalMs: 60_000, paused: true })
  const [more, setMore] = useState<{ items: CheckEvent[]; next: string | null } | null>(null)
  const [moreError, setMoreError] = useState<string | null>(null)
  const [loading, setLoading] = useState(false)

  const events = first.data ? [...first.data.items, ...(more?.items ?? [])] : null
  const next = more ? more.next : (first.data?.next_cursor ?? null)
  const error = moreError ?? first.error

  async function loadMore(cursor: string) {
    setLoading(true)
    setMoreError(null)
    try {
      const page = await listCheckEvents(hostId, checkId, cursor)
      setMore((prev) => ({ items: [...(prev?.items ?? []), ...page.items], next: page.next_cursor }))
    } catch (err) {
      setMoreError(err instanceof Error ? err.message : 'Error desconocido')
    } finally {
      setLoading(false)
    }
  }

  if (error && !events) return <InlineError>{error}</InlineError>
  if (!events) return <Skeleton className="h-12 w-full" />
  return (
    <div className="flex flex-col gap-2">
      {events.length === 0 ? (
        <p className="text-sm text-text-muted">Sin cambios de estado registrados.</p>
      ) : (
        <ul className="divide-y divide-border rounded-md border border-border bg-surface">
          {events.map((e) => (
            <li key={e.id} className="flex flex-wrap items-center gap-x-4 gap-y-1 px-3 py-2 text-sm">
              <span className="flex items-center gap-2">
                {e.from_status ? <CheckStatusBadge status={e.from_status} /> : <span className="text-sm text-text-muted">Primer resultado</span>}
                <ArrowRightIcon className="size-4 shrink-0 text-text-muted" strokeWidth={1.5} aria-label="a" />
                <CheckStatusBadge status={e.to_status} />
              </span>
              <span className="tabular-nums">{formatFullDate(e.at)}</span>
              <span className="text-sm text-text-muted">{detailLabel(e.detail)}</span>
            </li>
          ))}
        </ul>
      )}
      {error && <InlineError>{error}</InlineError>}
      {next && (
        <Button variant="outline" size="sm" className="w-fit" disabled={loading} onClick={() => void loadMore(next)}>
          {loading ? 'Cargando…' : 'Cargar más'}
        </Button>
      )}
    </div>
  )
}

function CheckRow({ n, hostId, item, now, targetFallback }: { n: number; hostId: string; item: HostCheck; now: number; targetFallback?: string }) {
  const [open, setOpen] = useState(false)
  const { check, state } = item
  // El ping no lleva destino propio: la direccion es la del dispositivo (solo la conoce un admin).
  const target = check.target || targetFallback || '—'
  return (
    <Fragment>
      <Row>
        <RowNumberCell n={n} />
        <Cell>
          <CheckStatusBadge status={check.enabled ? (state?.status ?? 'unknown') : 'paused'} />
        </Cell>
        <Cell className="max-w-56 truncate font-semibold">{check.name}</Cell>
        <Cell>{CHECK_TYPE_LABELS[check.type]}</Cell>
        <Cell className="max-w-64 truncate tabular-nums" title={target}>
          {target}
        </Cell>
        <Cell className="tabular-nums">{hostCheckValue(item)}</Cell>
        <Cell className="max-w-64 truncate" title={detailLabel(state?.detail ?? null)}>
          {detailLabel(state?.detail ?? null)}
        </Cell>
        <Cell className="tabular-nums">{formatSeconds(check.interval_s)}</Cell>
        <Cell className="tabular-nums">{state ? `hace ${formatElapsed(state.ts, now)}` : 'Sin resultados'}</Cell>
        <Cell className="tabular-nums">{state ? `hace ${formatElapsed(state.status_changed_at, now)}` : '—'}</Cell>
        <Cell>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-expanded={open}
            aria-label={`${open ? 'Ocultar' : 'Ver'} transiciones de ${check.name}`}
            title="Transiciones"
            onClick={() => setOpen((o) => !o)}
          >
            <ChevronDownIcon className={`transition-transform ${open ? '' : '-rotate-90'}`} strokeWidth={1.5} aria-hidden />
          </Button>
        </Cell>
      </Row>
      {open && (
        <tr>
          <td colSpan={11} className="border-t border-border bg-bg px-3 py-3">
            <EventsPanel hostId={hostId} checkId={check.id} />
          </td>
        </tr>
      )}
    </Fragment>
  )
}

function ChecksContent({ hostId, targetFallback }: { hostId: string; targetFallback?: string }) {
  const { data, error } = useHostChecks(hostId)
  const now = useNow(30_000)
  if (error && !data) return <InlineError>{error}</InlineError>
  if (!data) return <Skeleton className="h-16 w-full" />
  if (data.length === 0) return <EmptyState Icon={ActivityIcon} title="Sin chequeos" />
  return (
    <DataTable caption="Chequeos del host" className="min-w-[68rem]">
      <TableHeader>
        <HeadRow>
          <RowNumberHeadCell />
          <HeadCell>Estado</HeadCell>
          <HeadCell>Nombre</HeadCell>
          <HeadCell>Tipo</HeadCell>
          <HeadCell>Destino</HeadCell>
          <HeadCell>Latencia / valor</HeadCell>
          <HeadCell>Detalle</HeadCell>
          <HeadCell>Cada</HeadCell>
          <HeadCell>Último resultado</HeadCell>
          <HeadCell>Estado desde</HeadCell>
          <HeadCell>Transiciones</HeadCell>
        </HeadRow>
      </TableHeader>
      <TableBody>
        {data.map((item, i) => (
          <CheckRow key={item.check.id} n={i + 1} hostId={hostId} item={item} now={now} targetFallback={targetFallback} />
        ))}
      </TableBody>
    </DataTable>
  )
}

/** Sección "Chequeos" del detalle de host: estado actual por chequeo y transiciones bajo demanda. */
export function HostChecksSection({ hostId, targetFallback, title = true }: { hostId: string; targetFallback?: string; title?: boolean }) {
  return (
    <section className="flex flex-col gap-3" aria-labelledby="host-checks-title">
      <h2 id="host-checks-title" className={title ? 'text-sm font-semibold' : 'sr-only'}>
        Chequeos
      </h2>
      <ChecksContent hostId={hostId} targetFallback={targetFallback} />
    </section>
  )
}
