import { IconPencil } from '@tabler/icons-react'
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
}

export function HostHeader({ host, ip, canEdit, onEdit }: HostHeaderProps) {
  const seen = host.last_seen_at ? `Visto ${formatLastSeen(host.last_seen_at)}` : 'Sin reportes todavía'
  const line = [host.group?.name ?? 'Sin grupo', formatOs(host.os), ip, seen].filter(Boolean).join(' · ')
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <h1 className="min-w-0 truncate text-lg font-semibold">{host.hostname}</h1>
        <HostStatusBadge status={host.status} />
        {canEdit && (
          <Button variant="outline" size="sm" className="ml-auto" onClick={onEdit}>
            <IconPencil stroke={1.5} aria-hidden />
            Editar
          </Button>
        )}
      </div>
      <p className="text-sm text-text-muted tabular-nums">{line}</p>
      {(host.status === 'down' || host.status === 'offline') && (
        <p className="text-sm">{hostStatusDetail(host.status, host.last_seen_at)}</p>
      )}
    </div>
  )
}
