import { PauseIcon, PencilIcon, RefreshCwIcon, Trash2Icon, type LucideIcon } from 'lucide-react'
import type { HostStatus } from '../types/host.types'

export type HostActionId = 'edit' | 'disable' | 'reenroll' | 'delete'

export interface HostAction {
  id: HostActionId
  label: string
  Icon: LucideIcon
  /** Irreversible: boton rojo donde hay texto; en la tabla, mismo icono (confirma el dialogo). */
  destructive?: boolean
}

/**
 * Acciones de admin sobre un host: UNA sola lista para la tabla de Hosts y el detalle del host
 * (mismo orden, mismos nombres, mismos iconos).
 */
export function hostActions(h: { kind: 'agent' | 'device'; status: HostStatus }): HostAction[] {
  const agent = h.kind === 'agent'
  const disabled = h.status === 'disabled'
  const out: HostAction[] = [{ id: 'edit', label: 'Editar', Icon: PencilIcon }]
  if (agent && !disabled) out.push({ id: 'disable', label: 'Deshabilitar', Icon: PauseIcon })
  if (agent) out.push({ id: 'reenroll', label: disabled ? 'Reactivar' : 'Re-enrolar', Icon: RefreshCwIcon })
  out.push({ id: 'delete', label: 'Eliminar', Icon: Trash2Icon, destructive: true })
  return out
}
