import type { ApiError } from '@/lib/api'

/** Aviso (solo admin) cuando el colector de un dispositivo es de otro grupo: dato anterior a la regla de mismo grupo. */
export const COLLECTOR_MISMATCH_NOTICE = 'El colector es de otro grupo: el dispositivo no se sondea. Muévelos al mismo grupo.'

/** Cuenta de dispositivos del `details` de `collector_has_devices` (0 si no vino). */
function deviceCount(details: unknown): number {
  const n = (details as { devices?: unknown } | null | undefined)?.devices
  return typeof n === 'number' ? n : 0
}

/** Traduce los errores de la regla colector-grupo (422/409); null = no es de esta regla. */
export function mapCollectorRuleError(err: ApiError): string | null {
  if (err.code === 'collector_group_mismatch') {
    return 'El dispositivo y su colector deben estar en el mismo grupo: cambia el grupo del dispositivo o elige un colector de su grupo.'
  }
  if (err.code === 'collector_has_devices') {
    const n = deviceCount(err.details)
    const who = n > 0 ? `${n.toLocaleString('es-PE')} ${n === 1 ? 'dispositivo' : 'dispositivos'}` : 'dispositivos'
    return `Este host es colector de ${who}: muévelos al mismo grupo a la vez, cámbiales el colector o elimínalos antes.`
  }
  return null
}
