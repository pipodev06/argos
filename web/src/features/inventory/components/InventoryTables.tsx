import { TriangleAlertIcon, CircleCheckIcon, CircleOffIcon, CircleHelpIcon, LockIcon, PauseIcon, type LucideIcon as Icon } from 'lucide-react'
import { Badge, type BadgeTint } from '@/components/ui/badge'
import { Cell, DataTable, HeadCell, HeadRow, Row, RowNumberCell, RowNumberHeadCell, TableBody, TableHeader } from '@/components/data-table/DataTable'
import { rowNumber } from '@/lib/pagination'
import { formatFullDate, formatMemory, formatNumber } from '@/features/hosts/utils/format'
import type { InventoryChange, PortItem, ProcessItem, ServiceItem, ServiceState } from '../types/inventory.types'
import { describeChange, isExposed, KIND_LABELS, MODULE_LABELS, START_LABELS, STATE_LABELS } from '../utils/inventory'

function ExposureBadge({ bind }: { bind: string }) {
  return isExposed(bind) ? (
    <Badge Icon={TriangleAlertIcon} tint="warn">
      Expuesto
    </Badge>
  ) : (
    <Badge Icon={LockIcon}>Solo local</Badge>
  )
}

const STATE_STYLE: Record<ServiceState, { Icon: Icon; tint: BadgeTint }> = {
  running: { Icon: CircleCheckIcon, tint: 'ok' },
  stopped: { Icon: CircleOffIcon, tint: 'muted' },
  paused: { Icon: PauseIcon, tint: 'info' },
  other: { Icon: CircleHelpIcon, tint: 'muted' },
}

function ServiceStateBadge({ state }: { state: ServiceState }) {
  const s = STATE_STYLE[state] ?? STATE_STYLE.other
  return (
    <Badge Icon={s.Icon} tint={s.tint}>
      {STATE_LABELS[state] ?? STATE_LABELS.other}
    </Badge>
  )
}

export function PortsTable({ items, page, pageSize }: { items: PortItem[]; page: number; pageSize: number }) {
  return (
    <DataTable caption="Puertos en escucha" className="min-w-[40rem]">
      <TableHeader>
        <HeadRow>
          <RowNumberHeadCell />
          <HeadCell>Puerto</HeadCell>
          <HeadCell>Protocolo</HeadCell>
          <HeadCell>Dirección</HeadCell>
          <HeadCell>Exposición</HeadCell>
          <HeadCell>Proceso</HeadCell>
        </HeadRow>
      </TableHeader>
      <TableBody>
        {items.map((p, i) => (
          <Row key={`${p.proto}/${p.bind}/${p.port}`}>
            <RowNumberCell n={rowNumber(page, pageSize, i)} />
            <Cell className="font-semibold tabular-nums">{p.port}</Cell>
            <Cell className="uppercase">{p.proto}</Cell>
            <Cell className="tabular-nums">{p.bind}</Cell>
            <Cell>
              <ExposureBadge bind={p.bind} />
            </Cell>
            <Cell className={p.owner_known ? 'max-w-56 truncate' : 'text-text-muted'} title={p.owner_known ? p.proc : undefined}>
              {p.owner_known ? p.proc : 'Desconocido'}
            </Cell>
          </Row>
        ))}
      </TableBody>
    </DataTable>
  )
}

export function ServicesTable({ items, page, pageSize }: { items: ServiceItem[]; page: number; pageSize: number }) {
  return (
    <DataTable caption="Servicios" className="min-w-[44rem]">
      <TableHeader>
        <HeadRow>
          <RowNumberHeadCell />
          <HeadCell>Nombre</HeadCell>
          <HeadCell>Identificador</HeadCell>
          <HeadCell>Estado</HeadCell>
          <HeadCell>Inicio</HeadCell>
        </HeadRow>
      </TableHeader>
      <TableBody>
        {items.map((s, i) => (
          <Row key={s.name}>
            <RowNumberCell n={rowNumber(page, pageSize, i)} />
            <Cell className="max-w-64 truncate font-semibold" title={s.display}>
              {s.display || s.name}
            </Cell>
            <Cell className="max-w-56 truncate text-text-muted" title={s.name}>
              {s.name}
            </Cell>
            <Cell>
              <ServiceStateBadge state={s.state} />
            </Cell>
            <Cell>{START_LABELS[s.start] ?? START_LABELS.other}</Cell>
          </Row>
        ))}
      </TableBody>
    </DataTable>
  )
}

export function ProcessesTable({ items, page, pageSize }: { items: ProcessItem[]; page: number; pageSize: number }) {
  return (
    <DataTable caption="Procesos (agrupados por nombre)" className="min-w-[32rem]">
      <TableHeader>
        <HeadRow>
          <RowNumberHeadCell />
          <HeadCell>Nombre</HeadCell>
          <HeadCell>Instancias</HeadCell>
          <HeadCell>Memoria (RSS)</HeadCell>
        </HeadRow>
      </TableHeader>
      <TableBody>
        {items.map((p, i) => (
          <Row key={p.name}>
            <RowNumberCell n={rowNumber(page, pageSize, i)} />
            <Cell className="max-w-64 truncate font-semibold" title={p.name}>
              {p.name}
            </Cell>
            <Cell className="tabular-nums">{formatNumber(p.count, 0)}</Cell>
            <Cell className="tabular-nums">{formatMemory(p.rss_bytes / 1_048_576)}</Cell>
          </Row>
        ))}
      </TableBody>
    </DataTable>
  )
}

export function ChangesTable({ items, page, pageSize }: { items: InventoryChange[]; page: number; pageSize: number }) {
  return (
    <DataTable caption="Cambios de inventario" className="min-w-[48rem]">
      <TableHeader>
        <HeadRow>
          <RowNumberHeadCell />
          <HeadCell>Fecha</HeadCell>
          <HeadCell>Módulo</HeadCell>
          <HeadCell>Tipo</HeadCell>
          <HeadCell>Elemento</HeadCell>
          <HeadCell>Detalle</HeadCell>
        </HeadRow>
      </TableHeader>
      <TableBody>
        {items.map((c, i) => {
          const detail = describeChange(c.kind, c.before, c.after)
          return (
            <Row key={c.id}>
              <RowNumberCell n={rowNumber(page, pageSize, i)} />
              <Cell className="tabular-nums">{formatFullDate(c.at)}</Cell>
              <Cell>{MODULE_LABELS[c.module] ?? c.module}</Cell>
              <Cell>{KIND_LABELS[c.kind] ?? c.kind}</Cell>
              <Cell className="max-w-48 truncate" title={c.key}>
                {c.key}
              </Cell>
              <Cell className="max-w-80 truncate" title={detail}>
                {detail}
              </Cell>
            </Row>
          )
        })}
      </TableBody>
    </DataTable>
  )
}
