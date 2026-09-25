import type { MaintenanceInput, MaintenanceWindow } from '../types/alert.types'
import { MAX_NAME } from './ruleForm'
import { ALL_SCOPE, draftToScope, scopeToDraft, type ScopeDraft } from './scope'

const HOUR = 3_600_000
const DAY = 24 * HOUR
const MAX_SPAN = 31 * DAY
const MAX_SPAN_ALL = DAY
const MAX_PAST_START = HOUR

export interface MaintenanceDraft {
  name: string
  /** Valor de `datetime-local` (hora local, sin zona): "2026-09-24T14:30". */
  startsAt: string
  endsAt: string
  scope: ScopeDraft
}

export type MaintenanceErrors = Partial<Record<'name' | 'startsAt' | 'endsAt' | 'scope', string>>

const pad = (n: number) => String(n).padStart(2, '0')

/** Fecha -> valor de `datetime-local` en hora local. */
export function toLocalInput(d: Date): string {
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

export function maintenanceToDraft(w: MaintenanceWindow | null, now: number = Date.now()): MaintenanceDraft {
  if (w) {
    return {
      name: w.name,
      startsAt: toLocalInput(new Date(w.starts_at)),
      endsAt: toLocalInput(new Date(w.ends_at)),
      scope: scopeToDraft(w.scope),
    }
  }
  return { name: '', startsAt: toLocalInput(new Date(now)), endsAt: toLocalInput(new Date(now + HOUR)), scope: ALL_SCOPE }
}

/**
 * Valida con las mismas reglas del servidor (tope 31 d; "todos los hosts" máx. 24 h; al crear o mover fechas,
 * que no haya terminado ni empiece hace más de 1 h). En edición solo envía las fechas si cambiaron.
 */
export function buildMaintenanceInput(
  d: MaintenanceDraft,
  original: MaintenanceDraft | null,
  now: number = Date.now(),
): { input: MaintenanceInput } | { errors: MaintenanceErrors } {
  const errors: MaintenanceErrors = {}
  const name = d.name.trim()
  if (!name) errors.name = 'Ingresa un nombre.'
  else if (name.length > MAX_NAME) errors.name = `Máximo ${MAX_NAME} caracteres.`

  const scope = draftToScope(d.scope)
  if ('error' in scope) errors.scope = scope.error

  const start = d.startsAt ? new Date(d.startsAt) : null
  const end = d.endsAt ? new Date(d.endsAt) : null
  if (!start || Number.isNaN(start.getTime())) errors.startsAt = 'Indica el inicio.'
  if (!end || Number.isNaN(end.getTime())) errors.endsAt = 'Indica el fin.'
  const datesChanged = !original || original.startsAt !== d.startsAt || original.endsAt !== d.endsAt

  if (start && end && !errors.startsAt && !errors.endsAt) {
    const span = end.getTime() - start.getTime()
    if (span <= 0) errors.endsAt = 'El fin debe ser posterior al inicio.'
    else if (span > MAX_SPAN) errors.endsAt = 'La ventana no puede durar más de 31 días.'
    else if (d.scope.type === 'all' && span > MAX_SPAN_ALL) {
      errors.endsAt = 'Sobre todos los hosts no puede durar más de 24 horas: acota el alcance a un grupo.'
    } else if (datesChanged) {
      if (end.getTime() <= now) errors.endsAt = 'El fin ya pasó.'
      else if (start.getTime() < now - MAX_PAST_START) errors.startsAt = 'El inicio no puede ser anterior a 1 hora atrás.'
    }
  }

  if (Object.keys(errors).length > 0 || 'error' in scope || !start || !end) return { errors }
  const input: MaintenanceInput = { name, scope: scope.scope }
  if (datesChanged) {
    input.starts_at = start.toISOString()
    input.ends_at = end.toISOString()
  }
  return { input }
}
