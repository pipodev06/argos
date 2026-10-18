import { ViewAction } from '@/components/data-table/RowActions'
import { targetTypeLabel } from '../utils/labels'

/** Tipos cuyo nombre actual resuelve el servidor: si no existe, se marca "(eliminado)". */
const RESOLVABLE = new Set(['host', 'device', 'user', 'group', 'alert_rule', 'channel', 'maintenance', 'service_check', 'snmp_profile'])

interface AuditTargetProps {
  type: string | null
  id: string | null
  /** Nombre del servidor: actual o, si se borro, el guardado al registrar el evento. */
  name: string | null
  /** El objetivo todavia existe (`target_exists`). */
  exists: boolean
}

/** Tipo + nombre del objetivo; un host o dispositivo que existe lleva el ojo a su detalle. El id completo va en el detalle del evento. */
export function AuditTarget({ type, id, name, exists }: AuditTargetProps) {
  if (!type && !id) return <span aria-label="Sin objetivo">—</span>
  const linkable = exists && id && (type === 'host' || type === 'device')
  const deleted = !exists && !!id && !!type && RESOLVABLE.has(type)
  return (
    <span className="inline-flex items-center gap-1">
      {type && <span className="text-text-muted">{targetTypeLabel(type)}</span>}
      {name && <span className="font-semibold">{name}</span>}
      {deleted && <span className="text-text-muted">(eliminado)</span>}
      {linkable && <ViewAction to={`/${type === 'device' ? 'dispositivos' : 'hosts'}/${id}`} label={name ?? (type === 'device' ? 'dispositivo' : 'host')} />}
    </span>
  )
}
