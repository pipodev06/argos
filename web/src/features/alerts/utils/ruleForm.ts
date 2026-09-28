import type { AlertRule, ExpectedPort, AlertSeverity, RuleInput, RuleKind, RuleMetric, RuleOp } from '../types/alert.types'
import { formatExpectedPorts, parseExpectedPorts } from './expectedPorts'
import { isLoadMetric, isUpdatesKind, parseNumber } from './format'
import { ALL_SCOPE, draftToScope, scopeToDraft, type ScopeDraft } from './scope'

export const MAX_NAME = 80
const MAX_MINUTES = 1440 // 24 h (tope del servidor: 86 400 s)
const MIN_NO_DATA = 60
const MAX_NO_DATA = 30 * 24 * 3600
const MAX_LOAD = 1e6
const MAX_PENDING = 10_000
const MAX_DAYS = 365
const MAX_EOL_LEAD_DAYS = 730
export const MAX_ESCALATE_MIN = 10_080 // 7 días (tope del servidor: 604 800 s)

export const KIND_OPTIONS: readonly { value: RuleKind; label: string }[] = [
  { value: 'metric', label: 'Métrica sobre umbral' },
  { value: 'host_status', label: 'Host caído' },
  { value: 'no_data', label: 'Sin reportar (sin datos)' },
  { value: 'service_check', label: 'Chequeo de servicio' },
  { value: 'exposed_port', label: 'Puerto nuevo expuesto' },
  { value: 'pending_updates', label: 'Actualizaciones de seguridad pendientes' },
  { value: 'reboot_pending', label: 'Reinicio pendiente' },
  { value: 'os_eol', label: 'Fin de vida del SO' },
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
  /** Solo pending_updates. */
  durationDays: string
  resolveMin: string
  /** Minutos sin reconocer antes de escalar; vacío = sin escalamiento. */
  escalateMin: string
  includeOffline: boolean
  checkId: string
  warnOrDown: boolean
  expectedPorts: string
  scope: ScopeDraft
  severity: AlertSeverity
  enabled: boolean
}

export type RuleErrors = Partial<Record<'name' | 'threshold' | 'clear' | 'duration' | 'durationDays' | 'resolve' | 'escalate' | 'scope' | 'check' | 'ports', string>>

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
      durationDays: '0',
      resolveMin: '1',
      escalateMin: '',
      includeOffline: false,
      checkId: '',
      warnOrDown: false,
      expectedPorts: '',
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
    durationDays: String(rule.duration_days ?? 0),
    resolveMin: minutesText(rule.resolve_seconds),
    escalateMin: rule.escalate_after_seconds === null ? '' : minutesText(rule.escalate_after_seconds),
    includeOffline: rule.include_offline,
    checkId: rule.check_id ?? '',
    warnOrDown: rule.warn_or_down,
    expectedPorts: formatExpectedPorts(rule.expected_ports ?? []),
    scope: scopeToDraft(rule.scope),
    severity: rule.severity,
    enabled: rule.enabled,
  }
}

export interface RuleTemplate {
  id: string
  label: string
  apply: () => RuleDraft
}

/** Plantillas de reglas (solo al crear): parten de un borrador en blanco y sobrescriben los campos de arriba. */
export const RULE_TEMPLATES: readonly RuleTemplate[] = [
  {
    id: 'cpu_alto',
    label: 'CPU alto sostenido',
    apply: () => ({ ...ruleToDraft(null), kind: 'metric', metric: 'cpu_pct', op: 'gt', threshold: '90', durationMin: '5', severity: 'warn' }),
  },
  {
    id: 'mem_alta',
    label: 'Memoria alta',
    apply: () => ({ ...ruleToDraft(null), kind: 'metric', metric: 'mem_pct', op: 'gt', threshold: '90', durationMin: '5', severity: 'warn' }),
  },
  {
    id: 'disco_lleno',
    label: 'Disco casi lleno',
    apply: () => ({ ...ruleToDraft(null), kind: 'metric', metric: 'disk_max_pct', op: 'gt', threshold: '90', durationMin: '5', severity: 'critical' }),
  },
  {
    id: 'host_caido',
    label: 'Host caído',
    apply: () => ({ ...ruleToDraft(null), kind: 'host_status', severity: 'critical', durationMin: '5' }),
  },
  {
    id: 'sin_reportar',
    label: 'Sin reportar',
    apply: () => ({ ...ruleToDraft(null), kind: 'no_data', threshold: '300', severity: 'warn' }),
  },
  {
    id: 'updates_seguridad',
    label: 'Actualizaciones de seguridad pendientes',
    apply: () => ({ ...ruleToDraft(null), kind: 'pending_updates', threshold: '1', severity: 'warn' }),
  },
]

const COPY_SUFFIX = ' (copia)'

/** Borrador para crear una regla nueva a partir de otra: mismos campos, nombre "<nombre> (copia)" recortado a MAX_NAME. */
export function cloneToDraft(rule: AlertRule): RuleDraft {
  const draft = ruleToDraft(rule)
  return { ...draft, name: draft.name.slice(0, MAX_NAME - COPY_SUFFIX.length).trimEnd() + COPY_SUFFIX }
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

  // Reglas de parches: su duracion (si aplica) va en dias; `duration_seconds` se envia en 0.
  const updates = isUpdatesKind(d.kind)
  const duration = updates ? { seconds: 0 } : minutes(d.durationMin, 'Duración')
  if ('error' in duration) errors.duration = duration.error
  const resolve = minutes(d.resolveMin, 'Espera de resolución')
  if ('error' in resolve) errors.resolve = resolve.error
  // Escalamiento opcional: entero de minutos; vacío = sin plazo (al editar, null lo desactiva).
  let escalate: number | null = null
  if (d.escalateMin.trim() !== '') {
    const n = parseNumber(d.escalateMin)
    if (n === null || !Number.isInteger(n) || n < 1 || n > MAX_ESCALATE_MIN) errors.escalate = `Escalar: un entero de minutos entre 1 y ${MAX_ESCALATE_MIN.toLocaleString('es-PE')}.`
    else escalate = n * 60
  }
  // Una regla service_check hereda el alcance del chequeo: no lleva alcance propio.
  const inheritsScope = d.kind === 'service_check'
  const scope = inheritsScope ? null : draftToScope(d.scope)
  if (scope && 'error' in scope) errors.scope = scope.error
  if (inheritsScope && !d.checkId) errors.check = 'Elige un chequeo.'

  let expectedPorts: ExpectedPort[] = []
  if (d.kind === 'exposed_port') {
    const parsed = parseExpectedPorts(d.expectedPorts)
    if ('error' in parsed) errors.ports = parsed.error
    else expectedPorts = parsed.ports
  }

  let threshold: number | undefined
  let clear: number | null = null
  let durationDays = 0
  if (d.kind === 'no_data') {
    const n = parseNumber(d.threshold)
    if (n === null || n < MIN_NO_DATA || n > MAX_NO_DATA) {
      errors.threshold = `Segundos sin reportar: entre ${MIN_NO_DATA} y ${MAX_NO_DATA.toLocaleString('es-PE')}.`
    } else threshold = n
  } else if (updates) {
    const [min, max, label] =
      d.kind === 'pending_updates'
        ? [1, MAX_PENDING, 'Pendientes de seguridad']
        : d.kind === 'reboot_pending'
          ? [1, MAX_DAYS, 'Días con reinicio pendiente']
          : [0, MAX_EOL_LEAD_DAYS, 'Días de antelación']
    const n = parseNumber(d.threshold)
    if (n === null || !Number.isInteger(n) || n < min || n > max) errors.threshold = `${label}: un entero entre ${min} y ${max.toLocaleString('es-PE')}.`
    else threshold = n
    if (d.kind === 'pending_updates') {
      const days = parseNumber(d.durationDays)
      if (days === null || !Number.isInteger(days) || days < 0 || days > MAX_DAYS) errors.durationDays = `Duración: un entero de días entre 0 y ${MAX_DAYS}.`
      else durationDays = days
    }
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
  if (escalate !== null || editing) input.escalate_after_seconds = escalate
  if (scope && 'scope' in scope) {
    input.scope = scope.scope
    input.include_offline = d.includeOffline
  }
  if (d.kind === 'service_check') input.warn_or_down = d.warnOrDown
  if (d.kind === 'exposed_port') input.expected_ports = expectedPorts
  if (d.kind === 'no_data' || updates) input.threshold = threshold
  if (d.kind === 'pending_updates') input.duration_days = durationDays
  if (d.kind === 'metric') {
    input.op = d.op
    input.threshold = threshold
    // En PATCH null borra el umbral de recuperación; al crear simplemente no se envía.
    if (clear !== null || editing) input.clear_threshold = clear
  }
  return { input }
}
