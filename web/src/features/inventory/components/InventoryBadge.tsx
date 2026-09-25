import type { ComponentType } from 'react'

type IconType = ComponentType<{ className?: string; stroke?: number; 'aria-hidden'?: boolean }>

/** Pill tintada con icono + texto; el color de estado va solo en icono y tinte (mismo patron que HostStatusBadge). */
export function InventoryBadge({ Icon, label, tint, iconClass }: { Icon: IconType; label: string; tint: string; iconClass: string }) {
  return (
    <span className={`inline-flex items-center gap-1 rounded-md px-2 py-0.5 text-xs font-medium text-text ${tint}`}>
      <Icon className={`size-3.5 shrink-0 ${iconClass}`} stroke={1.5} aria-hidden />
      {label}
    </span>
  )
}
