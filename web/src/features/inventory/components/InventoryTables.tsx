import {
  IconAlertTriangle,
  IconCircleCheck,
  IconCircleOff,
  IconHelpCircle,
  IconLock,
  IconPlayerPause,
} from '@tabler/icons-react'
import { Cell, DataTable, HeadCell, HeadRow, Row } from '@/components/data-table/DataTable'
import { formatFullDate, formatMemory, formatNumber } from '@/features/hosts/utils/format'
import type { InventoryChange, PortItem, ProcessItem, ServiceItem, ServiceState } from '../types/inventory.types'
import { describeChange, isExposed, KIND_LABELS, MODULE_LABELS, START_LABELS, STATE_LABELS } from '../utils/inventory'
import { InventoryBadge } from './InventoryBadge'

function ExposureBadge({ bind }: { bind: string }) {
  return isExposed(bind) ? (
    <InventoryBadge Icon={IconAlertTriangle} label="Expuesto" tint="bg-warn/10" iconClass="text-warn" />
  ) : (
    <InventoryBadge Icon={IconLock} label="Solo local" tint="bg-text-muted/10" iconClass="text-text-muted" />
  )
}

const STATE_STYLE: Record<ServiceState, { Icon: typeof IconCircleCheck; tint: string; iconClass: string }> = {
  running: { Icon: IconCircleCheck, tint: 'bg-ok/10', iconClass: 'text-ok' },
  stopped: { Icon: IconCircleOff, tint: 'bg-text-muted/10', iconClass: 'text-text-muted' },
  paused: { Icon: IconPlayerPause, tint: 'bg-info/10', iconClass: 'text-info' },
  other: { Icon: IconHelpCircle, tint: 'bg-text-muted/10', iconClass: 'text-text-muted' },
}

function ServiceStateBadge({ state }: { state: ServiceState }) {
  const s = STATE_STYLE[state] ?? STATE_STYLE.other
  return <InventoryBadge Icon={s.Icon} label={STATE_LABELS[state] ?? STATE_LABELS.other} tint={s.tint} iconClass={s.iconClass} />
}

export function PortsTable({ items }: { items: PortItem[] }) {
  return (
    <DataTable caption="Puertos en escucha" className="min-w-[40rem]">
      <thead>
        <HeadRow>
          <HeadCell>Puerto</HeadCell>
          <HeadCell>Protocolo</HeadCell>
          <HeadCell>Dirección</HeadCell>
          <HeadCell>Exposición</HeadCell>
          <HeadCell>Proceso</HeadCell>
        </HeadRow>
      </thead>
      <tbody>
        {items.map((p) => (
          <Row key={`${p.proto}/${p.bind}/${p.port}`}>
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
      </tbody>
    </DataTable>
  )
}

export function ServicesTable({ items }: { items: ServiceItem[] }) {
  return (
    <DataTable caption="Servicios" className="min-w-[44rem]">
      <thead>
        <HeadRow>
          <HeadCell>Nombre</HeadCell>
          <HeadCell>Identificador</HeadCell>
          <HeadCell>Estado</HeadCell>
          <HeadCell>Inicio</HeadCell>
        </HeadRow>
      </thead>
      <tbody>
        {items.map((s) => (
          <Row key={s.name}>
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
      </tbody>
    </DataTable>
  )
}

export function ProcessesTable({ items }: { items: ProcessItem[] }) {
  return (
    <DataTable caption="Procesos (agrupados por nombre)" className="min-w-[32rem]">
      <thead>
        <HeadRow>
          <HeadCell>Nombre</HeadCell>
          <HeadCell>Instancias</HeadCell>
          <HeadCell>Memoria (RSS)</HeadCell>
        </HeadRow>
      </thead>
      <tbody>
        {items.map((p) => (
          <Row key={p.name}>
            <Cell className="max-w-64 truncate font-semibold" title={p.name}>
              {p.name}
            </Cell>
            <Cell className="tabular-nums">{formatNumber(p.count, 0)}</Cell>
            <Cell className="tabular-nums">{formatMemory(p.rss_bytes / 1_048_576)}</Cell>
          </Row>
        ))}
      </tbody>
    </DataTable>
  )
}

export function ChangesTable({ items }: { items: InventoryChange[] }) {
  return (
    <DataTable caption="Cambios de inventario" className="min-w-[48rem]">
      <thead>
        <HeadRow>
          <HeadCell>Fecha</HeadCell>
          <HeadCell>Módulo</HeadCell>
          <HeadCell>Tipo</HeadCell>
          <HeadCell>Elemento</HeadCell>
          <HeadCell>Detalle</HeadCell>
        </HeadRow>
      </thead>
      <tbody>
        {items.map((c) => {
          const detail = describeChange(c.kind, c.before, c.after)
          return (
            <Row key={c.id}>
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
      </tbody>
    </DataTable>
  )
}
