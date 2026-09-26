import type { Channel } from '../types/channel.types'

/** Valor del selector para "sin dueño" (canal global); un grupo usa su id. */
export const GLOBAL_OWNER = 'global'

export function ownerValue(c: Pick<Channel, 'group_id'> | null): string {
  return c?.group_id ?? GLOBAL_OWNER
}

/** Valor del selector a `group_id` del contrato (null = global). */
export function ownerToGroupId(value: string): string | null {
  return value === GLOBAL_OWNER ? null : value
}

/** Texto de la columna "Dueño". */
export function ownerLabel(c: Pick<Channel, 'group_id' | 'group_name'>): string {
  return c.group_id ? (c.group_name ?? 'Grupo') : 'Global'
}

interface ScopeRule {
  id: string
  name: string
}

/** Lee `details.rules` de un `channel_scope_mismatch`; ignora formas inesperadas. */
export function scopeMismatchRules(details: unknown): string[] {
  if (typeof details !== 'object' || details === null) return []
  const rules = (details as { rules?: unknown }).rules
  if (!Array.isArray(rules)) return []
  return rules
    .filter((r): r is ScopeRule => typeof r === 'object' && r !== null && typeof (r as ScopeRule).name === 'string')
    .map((r) => r.name)
}

/** Mensaje accionable de `channel_scope_mismatch`. El servidor lista hasta 20 reglas. */
export function describeScopeMismatch(details: unknown): string {
  const names = scopeMismatchRules(details)
  const base = 'Un canal de un cliente solo puede usarse en reglas de ese cliente.'
  const list = names.length ? ` Reglas implicadas: ${names.join(', ')}${names.length >= 20 ? ' y quizá más' : ''}.` : ''
  return `${base}${list} Cambia el dueño del canal a Global o quita el canal de esas reglas.`
}
