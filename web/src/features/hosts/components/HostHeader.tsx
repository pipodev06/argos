import { Button } from '@/components/ui/button'
import type { HostDetail } from '../types/host.types'
import { formatLastSeen } from '../utils/format'
import { hostActions, type HostActionId } from '../utils/hostActions'
import { formatOs } from '../utils/os'
import { hostStatusDetail } from '../utils/status'
import { HostStatusBadge } from './HostStatusBadge'

interface HostHeaderProps {
  host: HostDetail
  /** IP principal (primera de `info.ips`). */
  ip: string | null
  /** Acciones de admin (las mismas que en la tabla de Hosts); sin ella no se muestran. */
  onAction?: (action: HostActionId) => void
}

export function HostHeader({ host, ip, onAction }: HostHeaderProps) {
  const seen = host.last_seen_at ? `Visto ${formatLastSeen(host.last_seen_at)}` : 'Sin reportes'
  const line = [host.group?.name ?? 'Sin grupo', formatOs(host.os), ip, seen].filter(Boolean).join(' · ')
  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <h1 className="min-w-0 truncate text-lg font-semibold">{host.hostname}</h1>
        <HostStatusBadge status={host.status} />
        {onAction && (
          <div className="ml-auto flex gap-2">
            {hostActions(host).map(({ id, label, Icon, destructive }) => (
              <Button key={id} variant={destructive ? 'destructive' : 'outline'} size="sm" onClick={() => onAction(id)}>
                <Icon strokeWidth={1.5} aria-hidden />
                {label}
              </Button>
            ))}
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
