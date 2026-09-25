import type { Device } from '../types/device.types'

export const NOT_EVALUATED_LABEL = 'Sin evaluar'
export const NOT_EVALUATED_REASON = 'El colector no está activo'

/** "Sin evaluar" + motivo si el colector del dispositivo no esta activo; null = mostrar el estado normal. */
export function deviceStateOverride(d: Pick<Device, 'evaluated' | 'status'>): { label: string; reason: string } | null {
  if (d.evaluated || d.status === 'disabled') return null
  return { label: NOT_EVALUATED_LABEL, reason: NOT_EVALUATED_REASON }
}
