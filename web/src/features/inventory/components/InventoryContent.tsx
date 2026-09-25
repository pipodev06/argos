import { useEffect, type ReactNode } from 'react'
import { IconAlertTriangle, IconListDetails, IconRefresh } from '@tabler/icons-react'
import { Button } from '@/components/ui/button'
import { EmptyState } from '@/components/ui/empty-state'
import { InlineError } from '@/components/ui/inline-error'
import { Pagination } from '@/components/ui/pagination'
import { SegmentedControl } from '@/components/ui/segmented-control'
import { Skeleton } from '@/components/ui/skeleton'
import { formatElapsed } from '@/features/hosts/utils/format'
import { useCursorPagination } from '@/hooks/useCursorPagination'
import { useNow } from '@/hooks/useNow'
import { usePagedList } from '@/hooks/usePagedList'
import { useUrlFilters } from '@/hooks/useUrlFilters'
import { useInventoryChanges, useInventoryModule } from '../hooks/useInventory'
import type { ModuleList } from '../types/inventory.types'
import {
  EMPTY_TEXT,
  emptyKind,
  INVENTORY_FIELDS,
  KIND_LABELS,
  MODULE_LABELS,
  SECTION_LABELS,
  SECTIONS,
  STATE_LABELS,
} from '../utils/inventory'
import { ChangesTable, PortsTable, ProcessesTable, ServicesTable } from './InventoryTables'

const SECTION_OPTIONS = SECTIONS.map((value) => ({ value, label: SECTION_LABELS[value] }))
const MODULE_OPTIONS = (['all', 'ports', 'services'] as const).map((value) => ({ value, label: MODULE_LABELS[value] }))
const KIND_OPTIONS = (['all', 'added', 'removed', 'changed', 'overflow'] as const).map((value) => ({ value, label: KIND_LABELS[value] }))
const STATE_OPTIONS = (['all', 'running', 'stopped', 'paused', 'other'] as const).map((value) => ({ value, label: STATE_LABELS[value] }))

interface QueryState<T> {
  data: T | null
  error: string | null
  errorStatus: number | null
}

/** Un 404 del servidor = sin acceso (o host fuera de ámbito): se oculta la sección sin revelar nada. */
function useDenied(errorStatus: number | null, onDenied: () => void) {
  useEffect(() => {
    if (errorStatus === 404) onDenied()
  }, [errorStatus, onDenied])
}

function ModuleBody<T>({
  query,
  onDenied,
  render,
}: {
  query: QueryState<ModuleList<T>>
  onDenied: () => void
  render: (items: T[]) => ReactNode
}) {
  const now = useNow(30_000)
  const { data, error, errorStatus } = query
  useDenied(errorStatus, onDenied)

  if (errorStatus === 404) return null
  if (error && !data) return <InlineError>{error}</InlineError>
  if (!data) return <Skeleton className="h-16 w-full" />

  const empty = emptyKind(data.status, data.count)
  if (empty) {
    const { title, description } = EMPTY_TEXT[empty]
    return <EmptyState Icon={IconListDetails} title={title} description={description} />
  }
  const stamp = data.updated_at ?? data.collected_at
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-text-muted">
        {stamp && <span className="tabular-nums">Actualizado hace {formatElapsed(stamp, now)}</span>}
        {data.truncated && (
          <span className="inline-flex items-center gap-1">
            <IconAlertTriangle className="size-4 shrink-0 text-warn" stroke={1.5} aria-hidden />
            Lista truncada
          </span>
        )}
      </div>
      {render(data.items)}
    </div>
  )
}

/** Lista completa paginada en cliente (15 por defecto). Remontar con `key` para volver a la página 1. */
function PagedList<T>({ items, itemLabel, render }: { items: T[]; itemLabel: string; render: (rows: T[], page: number, pageSize: number) => ReactNode }) {
  const paged = usePagedList(items)
  return (
    <div className="flex flex-col gap-3">
      {render(paged.rows, paged.page, paged.pageSize)}
      <Pagination
        page={paged.page}
        pageSize={paged.pageSize}
        count={paged.rows.length}
        total={paged.total}
        itemLabel={itemLabel}
        hasPrev={paged.hasPrev}
        hasNext={paged.hasNext}
        onPrev={paged.prev}
        onNext={paged.next}
        onPageSizeChange={paged.setPageSize}
      />
    </div>
  )
}

function PortsView({ hostId, onDenied }: { hostId: string; onDenied: () => void }) {
  const query = useInventoryModule(hostId, 'ports')
  return <ModuleBody query={query} onDenied={onDenied} render={(items) => <PagedList items={items} itemLabel="puertos" render={(rows, page, pageSize) => <PortsTable items={rows} page={page} pageSize={pageSize} />} />} />
}

function ServicesView({ hostId, state, onDenied }: { hostId: string; state: string; onDenied: () => void }) {
  const query = useInventoryModule(hostId, 'services')
  return (
    <ModuleBody
      query={query}
      onDenied={onDenied}
      render={(items) => {
        const shown = state === 'all' ? items : items.filter((s) => s.state === state)
        return shown.length === 0 ? (
          <EmptyState Icon={IconListDetails} title="Ningún servicio coincide con el estado elegido" />
        ) : (
          <PagedList key={state} items={shown} itemLabel="servicios" render={(rows, page, pageSize) => <ServicesTable items={rows} page={page} pageSize={pageSize} />} />
        )
      }}
    />
  )
}

function ProcessesView({ hostId, onDenied }: { hostId: string; onDenied: () => void }) {
  const query = useInventoryModule(hostId, 'processes')
  return <ModuleBody query={query} onDenied={onDenied} render={(items) => <PagedList items={items} itemLabel="procesos" render={(rows, page, pageSize) => <ProcessesTable items={rows} page={page} pageSize={pageSize} />} />} />
}

function ChangesView({
  hostId,
  module,
  kind,
  pagination,
  onDenied,
}: {
  hostId: string
  module: string
  kind: string
  pagination: ReturnType<typeof useCursorPagination>
  onDenied: () => void
}) {
  const { data, error, errorStatus, loading, refresh } = useInventoryChanges(hostId, module, kind, pagination.cursor, pagination.pageSize)
  useDenied(errorStatus, onDenied)

  // 400 con cursor = cursor invalido (p. ej. reinicio del servidor): se vuelve a la primera pagina.
  const resetPagination = pagination.reset
  const staleCursor = errorStatus === 400 && pagination.cursor !== null
  useEffect(() => {
    if (staleCursor) resetPagination()
  }, [staleCursor, resetPagination])

  if (errorStatus === 404) return null
  if (error && !data) return <InlineError>{error}</InlineError>
  if (!data) return <Skeleton className="h-16 w-full" />
  return (
    <div className="flex flex-col gap-3" aria-busy={loading}>
      <div>
        <Button variant="outline" size="sm" onClick={() => void refresh()}>
          <IconRefresh stroke={1.5} aria-hidden />
          Actualizar
        </Button>
      </div>
      {data.items.length === 0 && pagination.page === 1 ? (
        <EmptyState Icon={IconListDetails} title="Sin cambios registrados" />
      ) : (
        <>
          <ChangesTable items={data.items} page={pagination.page} pageSize={pagination.pageSize} />
          <Pagination
            page={pagination.page}
            pageSize={pagination.pageSize}
            count={data.items.length}
            itemLabel="cambios"
            hasPrev={pagination.hasPrev}
            hasNext={data.next_cursor !== null}
            onPrev={pagination.prev}
            onNext={() => data.next_cursor && pagination.next(data.next_cursor)}
            onPageSizeChange={pagination.setPageSize}
            loading={loading}
          />
        </>
      )}
    </div>
  )
}

/** Contenido de la página de inventario: una sola consulta por sub-sección activa. */
export function InventoryContent({ hostId, onDenied }: { hostId: string; onDenied: () => void }) {
  const pagination = useCursorPagination()
  const { filters, setFilters } = useUrlFilters(INVENTORY_FIELDS, { onChange: pagination.reset })
  const { section } = filters

  return (
    <>
      <div className="flex flex-wrap items-center gap-3">
        <SegmentedControl aria-label="Sección de inventario" options={SECTION_OPTIONS} value={section} onChange={(v) => setFilters({ section: v })} />
        {section === 'services' && (
          <SegmentedControl aria-label="Filtrar por estado" options={STATE_OPTIONS} value={filters.state} onChange={(v) => setFilters({ state: v })} />
        )}
        {section === 'changes' && (
          <>
            <SegmentedControl aria-label="Filtrar por módulo" options={MODULE_OPTIONS} value={filters.module} onChange={(v) => setFilters({ module: v })} />
            <SegmentedControl aria-label="Filtrar por tipo" options={KIND_OPTIONS} value={filters.kind} onChange={(v) => setFilters({ kind: v })} />
          </>
        )}
      </div>
      {section === 'ports' && <PortsView hostId={hostId} onDenied={onDenied} />}
      {section === 'services' && <ServicesView hostId={hostId} state={filters.state} onDenied={onDenied} />}
      {section === 'processes' && <ProcessesView hostId={hostId} onDenied={onDenied} />}
      {section === 'changes' && (
        <ChangesView hostId={hostId} module={filters.module} kind={filters.kind} pagination={pagination} onDenied={onDenied} />
      )}
    </>
  )
}

