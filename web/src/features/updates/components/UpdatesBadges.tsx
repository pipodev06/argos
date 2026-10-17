import { TriangleAlertIcon, CircleCheckIcon, CircleXIcon, CircleHelpIcon, RotateCwIcon } from 'lucide-react'
import type { LucideIcon as Icon } from 'lucide-react'
import type { Eol } from '../types/updates.types'
import { eolInfo, formatDataAge, rebootInfo, type EolState, type RebootState } from '../utils/updates'

/** Icono de estado + texto (el estado nunca depende solo del color; el color va solo en el icono). */
function IconText({ Icon, iconClassName, children }: { Icon: Icon; iconClassName?: string; children: string }) {
  return (
    <span className="inline-flex items-center justify-center gap-1.5">
      <Icon className={`size-4 shrink-0 ${iconClassName ?? ''}`} strokeWidth={1.5} aria-hidden />
      <span>{children}</span>
    </span>
  )
}

const REBOOT_ICON: Record<RebootState, { Icon: Icon; className: string }> = {
  yes: { Icon: RotateCwIcon, className: 'text-warn' },
  no: { Icon: CircleCheckIcon, className: 'text-ok' },
  unknown: { Icon: CircleHelpIcon, className: 'text-text-muted' },
}

const EOL_ICON: Record<EolState, { Icon: Icon; className: string }> = {
  ok: { Icon: CircleCheckIcon, className: 'text-ok' },
  soon: { Icon: TriangleAlertIcon, className: 'text-warn' },
  expired: { Icon: CircleXIcon, className: 'text-danger' },
  none: { Icon: CircleHelpIcon, className: 'text-text-muted' },
}

export function RebootText({ required, since, now }: { required: boolean | null; since: string | null; now?: number }) {
  const { state, text } = rebootInfo(required, since, now)
  const { Icon, className } = REBOOT_ICON[state]
  return (
    <IconText Icon={Icon} iconClassName={className}>
      {text}
    </IconText>
  )
}

export function EolText({ eol }: { eol: Eol | null }) {
  const { state, text } = eolInfo(eol)
  const { Icon, className } = EOL_ICON[state]
  return (
    <IconText Icon={Icon} iconClassName={className}>
      {text}
    </IconText>
  )
}

/** "hace 5 min" y, si la cache de paquetes es vieja, el aviso "desactualizado" con icono. */
export function DataAgeText({ seconds, stale }: { seconds: number; stale: boolean }) {
  return (
    <span className="inline-flex flex-col items-center gap-0.5">
      <span>{formatDataAge(seconds)}</span>
      {stale && (
        <span className="inline-flex items-center gap-1 text-xs text-text-muted">
          <TriangleAlertIcon className="size-3.5 shrink-0 text-warn" strokeWidth={1.5} aria-hidden />
          desactualizado
        </span>
      )}
    </span>
  )
}
