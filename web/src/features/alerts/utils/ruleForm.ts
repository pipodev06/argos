import type { AlertRule, AlertSeverity, RuleInput, RuleKind, RuleMetric, RuleOp } from '../types/alert.types'
import { isLoadMetric, parseNumber } from './format'
import { ALL_SCOPE, draftToScope, scopeToDraft, type ScopeDraft } from './scope'

export const MAX_NAME = 80
const MAX_MINUTES = 1440 // 24 h (tope del servidor: 86 400 s)
const MIN_NO_DATA = 60
const MAX_NO_DATA = 30 * 24 * 3600
const MAX_LOAD = 1e6

export const KIND_OPTIONS: readonly { value: RuleKind; label: string }[] = [
  { value: 'metric', label: 'Métrica sobre umbral' },
  { value: 'host_status', label: 'Host caído' },
  { value: 'no_data', label: 'Sin reportar (sin datos)' },
  { value: 'service_check', label: 'Chequeo de servicio' },
]
export const OP_OPTIONS: readonly { value: RuleOp; label: string }[] = [
  { value: 'gt', label: 'Mayor que (>)' },
  { value: 'lt', label: 'Menor que (<)' },
]

export interface RuleDraft {
  name: string
  kind: RuleKind
  metric: RuleMetric
  op: RuleOp
  threshold: string
  clear: string
  durationMin: string
  resolveMin: string
  includeOffline: boolean
  checkId: string
  warnOrDown: boolean
  scope: ScopeDraft
  severity: AlertSeverity
  enabled: boolean
}

export type RuleErrors = Partial<Record<'name' | 'threshold' | 'clear' | 'duration' | 'resolve' | 'scope' | 'check', string>>

const minutesText = (seconds: number) => String(Math.round((seconds / 60) * 100) / 100)

export function ruleToDraft(rule: AlertRule | null): RuleDraft {
  if (!rule) {
    return {
      name: '',
      kind: 'metric',
      metric: 'cpu_pct',
      op: 'gt',
      threshold: '',
      clear: '',
      durationMin: '5',
      resolveMin: '1',
      includeOffline: false,
      checkId: '',
      warnOrDown: false,
      scope: ALL_SCOPE,
      severity: 'warn',
      enabled: true,
    }
  }
  return {
    name: rule.name,
    kind: rule.kind,
    metric: rule.metric ?? 'cpu_pct',
    op: rule.op ?? 'gt',
    threshold: rule.threshold === null ? '' : String(rule.threshold),
    clear: rule.clear_threshold === null ? '' : String(rule.clear_threshold),
    durationMin: minutesText(rule.duration_seconds),
    resolveMin: minutesText(rule.resolve_seconds),
    includeOffline: rule.include_offline,
    checkId: rule.check_id ?? '',
    warnOrDown: rule.warn_or_down,
    scope: scopeToDraft(rule.scope),
    severity: rule.severity,
    enabled: rule.enabled,
  }
}

function minutes(raw: string, label: string): { seconds: number } | { error: string } {
  const n = parseNumber(raw)
  if (n === null || n < 0 || n > MAX_MINUTES) return { error: `${label}: un número de minutos entre 0 y ${MAX_MINUTES}.` }
  return { seconds: Math.round(n * 60) }
}

/** Valida el borrador y arma el cuerpo de la API (`kind` y `metric` los agrega quien crea; nunca en PATCH). */
export function buildRuleInput(d: RuleDraft, editing: boolean): { input: RuleInput } | { errors: RuleErrors } {
  const errors: RuleErrors = {}
  const name = d.name.trim()
  if (!name) errors.name = 'Ingresa un nombre.'
  else if (name.length > MAX_NAME) errors.name = `Máximo ${MAX_NAME} caracteres.`

  const duration = minutes(d.durationMin, 'Duración')
  if ('error' in duration) errors.duration = duration.error
  const resolve = minutes(d.resolveMin, 'Espera de resolución')
  if ('error' in resolve) errors.resolve = resolve.error
  // Una regla service_check hereda el alcance del chequeo: no lleva alcance propio.
  const inheritsScope = d.kind === 'service_check'
  const scope = inheritsScope ? null : draftToScope(d.scope)
  if (scope && 'error' in scope) errors.scope = scope.error
  if (inheritsScope && !d.checkId) errors.check = 'Elige un chequeo.'

  let threshold: number | undefined
  let clear: number | null = null
  if (d.kind === 'no_data') {
    const n = parseNumber(d.threshold)
    if (n === null || n < MIN_NO_DATA || n > MAX_NO_DATA) {
      errors.threshold = `Segundos sin reportar: entre ${MIN_NO_DATA} y ${MAX_NO_DATA.toLocaleString('es-PE')}.`
    } else threshold = n
  } else if (d.kind === 'metric') {
    const max = isLoadMetric(d.metric) ? MAX_LOAD : 100
    const n = parseNumber(d.threshold)
    if (n === null || n < 0 || n > max) errors.threshold = `Umbral: un número entre 0 y ${max.toLocaleString('es-PE')}.`
    else threshold = n
    if (d.clear.trim() !== '') {
      const c = parseNumber(d.clear)
      if (c === null || c < 0 || c > max) errors.clear = `Umbral de recuperación: un número entre 0 y ${max.toLocaleString('es-PE')}.`
      else if (threshold !== undefined && ((d.op === 'gt' && c > threshold) || (d.op === 'lt' && c < threshold))) {
        errors.clear = `Debe quedar del lado sano del umbral (${d.op === 'gt' ? 'menor o igual' : 'mayor o igual'}).`
      } else clear = c
    }
  }

  if (Object.keys(errors).length > 0 || 'error' in duration || 'error' in resolve || (scope && 'error' in scope)) return { errors }
  const input: RuleInput = {
    name,
    duration_seconds: duration.seconds,
    resolve_seconds: resolve.seconds,
    severity: d.severity,
    enabled: d.enabled,
  }
  if (scope && 'scope' in scope) {
    input.scope = scope.scope
    input.include_offline = d.includeOffline
  }
  if (d.kind === 'service_check') input.warn_or_down = d.warnOrDown
  if (d.kind === 'no_data') input.threshold = threshold
  if (d.kind === 'metric') {
    input.op = d.op
    input.threshold = threshold
    // En PATCH null borra el umbral de recuperación; al crear simplemente no se envía.
    if (clear !== null || editing) input.clear_threshold = clear
  }
  return { input }
}
