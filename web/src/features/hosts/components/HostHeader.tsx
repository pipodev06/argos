import { IconEraser, IconPencil, IconPlayerPause, IconRefresh } from '@tabler/icons-react'
import { Button } from '@/components/ui/button'
import type { HostDetail } from '../types/host.types'
import { formatLastSeen } from '../utils/format'
import { formatOs } from '../utils/os'
import { hostStatusDetail } from '../utils/status'
import { HostStatusBadge } from './HostStatusBadge'

interface HostHeaderProps {
  host: HostDetail
  /** IP principal (primera de `info.ips`). */
  ip: string | null
  canEdit: boolean
  onEdit: () => void
  /** Baja del host y de todos sus datos (solo admin). */
  onPurge?: () => void
  /** Deshabilita el host (solo host `agent` no deshabilitado; solo admin). */
  onRevoke?: () => void
  /** Reactiva (si esta `disabled`) o re-enrola (token nuevo) el host `agent`; solo admin. */
  onReEnroll?: () => void
}

export function HostHeader({ host, ip, canEdit, onEdit, onPurge, onRevoke, onReEnroll }: HostHeaderProps) {
  const seen = host.last_seen_at ? `Visto ${formatLastSeen(host.last_seen_at)}` : 'Sin reportes'
  const line = [host.group?.name ?? 'Sin grupo', formatOs(host.os), ip, seen].filter(Boolean).join(' · ')
  const isAgent = host.kind === 'agent'
  const isDisabled = host.status === 'disabled'
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <h1 className="min-w-0 truncate text-lg font-semibold">{host.hostname}</h1>
        <HostStatusBadge status={host.status} />
        {canEdit && (
          <div className="ml-auto flex gap-2">
            <Button variant="outline" size="sm" onClick={onEdit}>
              <IconPencil stroke={1.5} aria-hidden />
              Editar
            </Button>
            {isAgent && onRevoke && !isDisabled && (
              <Button variant="outline" size="sm" onClick={onRevoke}>
                <IconPlayerPause stroke={1.5} aria-hidden />
                Deshabilitar
              </Button>
            )}
            {isAgent && onReEnroll && (
              <Button variant="outline" size="sm" onClick={onReEnroll}>
                <IconRefresh stroke={1.5} aria-hidden />
                {isDisabled ? 'Reactivar' : 'Re-enrolar'}
              </Button>
            )}
            {onPurge && (
              <Button variant="outline" size="sm" onClick={onPurge}>
                <IconEraser stroke={1.5} aria-hidden />
                Eliminar datos del host…
              </Button>
            )}
          </div>
        )}
      </div>
      <p className="text-sm text-text-muted tabular-nums">{line}</p>
      {(host.status === 'down' || host.status === 'offline') && (
        <p className="text-sm">{hostStatusDetail(host.status, host.last_seen_at)}</p>
      )}
    </div>
  )
}
