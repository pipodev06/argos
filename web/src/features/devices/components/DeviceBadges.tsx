import { IconAlertTriangle, IconPlugConnectedX, IconRouter } from '@tabler/icons-react'
import { Badge } from '@/components/ui/badge'
import { HostStatusBadge } from '@/features/hosts/components/HostStatusBadge'
import type { Device } from '../types/device.types'
import { COLLECTOR_MISMATCH_NOTICE } from '../utils/collectorRule'
import { deviceStateOverride } from '../utils/status'

/** Etiqueta "Dispositivo" (icono + texto) para distinguir un dispositivo sin agente de un host con agente. */
export function DeviceKindBadge() {
  return <Badge Icon={IconRouter}>Dispositivo</Badge>
}

/** Estado de un dispositivo: el de un host, o "Sin evaluar" (muted, con el motivo) si su colector no esta activo. */
export function DeviceStatusBadge({ device }: { device: Pick<Device, 'evaluated' | 'status'> }) {
  const override = deviceStateOverride(device)
  if (!override) return <HostStatusBadge status={device.status} />
  return (
    <Badge Icon={IconPlugConnectedX} title={override.reason}>
      {override.label}
      <span className="sr-only">: {override.reason}</span>
    </Badge>
  )
}

/** Aviso de colector de otro grupo (icono + texto). `compact` (tablas): etiqueta corta con el aviso completo como texto para lectores. */
export function CollectorMismatchNotice({ compact = false }: { compact?: boolean }) {
  return (
    <span className="inline-flex items-center gap-1 text-xs">
      <IconAlertTriangle className="size-3.5 shrink-0 text-warn" stroke={1.5} aria-hidden />
      {compact ? (
        <>
          <span title={COLLECTOR_MISMATCH_NOTICE}>Colector de otro grupo</span>
          <span className="sr-only">: {COLLECTOR_MISMATCH_NOTICE}</span>
        </>
      ) : (
        COLLECTOR_MISMATCH_NOTICE
      )}
    </span>
  )
}
