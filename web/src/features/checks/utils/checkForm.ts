import { draftToScope, scopeToDraft, ALL_SCOPE, type ScopeDraft } from '@/features/alerts/utils/scope'
import type { CheckInput, CheckType, ServiceCheck } from '../types/check.types'

export const MAX_NAME = 80
export const MIN_INTERVAL = 30
export const MAX_INTERVAL = 86400
export const MIN_TIMEOUT = 200
export const MAX_TIMEOUT = 30000
export const MAX_RETRIES = 3
export const MAX_LATENCY = 60000
export const MAX_DAYS = 365
export const MAX_URL = 512
export const MAX_EXPECT_TEXT = 100
export const MAX_SECRET = 512
export const HEADER_OPTIONS = ['Authorization', 'X-API-Key', 'X-Auth-Token'] as const

const HOST_RE = /^[A-Za-z0-9._:-]+$/
const SERVICE_RE = /^[A-Za-z0-9@._:-]{1,64}$/
const STATUS_RE = /^(\d{3})(?:-(\d{3}))?$/
// Mismas claves que rechaza el servidor en la query de la URL.
const SECRET_KEYS = ['token', 'key', 'secret', 'pass', 'pwd', 'auth', 'sig', 'cred', 'bearer', 'jwt', 'session', 'sid']

export const CHECK_TYPE_OPTIONS: readonly { value: CheckType; label: string }[] = [
  { value: 'tcp', label: 'TCP (puerto)' },
  { value: 'http', label: 'HTTP (URL)' },
  { value: 'tls_cert', label: 'Certificado TLS' },
  { value: 'service', label: 'Servicio del sistema' },
]

export interface CheckDraft {
  name: string
  type: CheckType
  interval: string
  timeout: string
  retries: string
  host: string
  port: string
  url: string
  expectStatus: string
  expectText: string
  serviceName: string
  warnMs: string
  critMs: string
  warnDays: string
  critDays: string
  headerName: string
  /** Solo escritura: nunca se rellena con el valor guardado. */
  secret: string
  removeSecret: boolean
  scope: ScopeDraft
  enabled: boolean
}

export type CheckErrors = Partial<Record<keyof CheckDraft | 'scope', string>>

const txt = (v: number | null): string => (v === null ? '' : String(v))

export function checkToDraft(c: ServiceCheck | null): CheckDraft {
  const base: CheckDraft = {
    name: '',
    type: 'tcp',
    interval: '60',
    timeout: '5000',
    retries: '1',
    host: '',
    port: '',
    url: '',
    expectStatus: '',
    expectText: '',
    serviceName: '',
    warnMs: '',
    critMs: '',
    warnDays: '30',
    critDays: '7',
    headerName: '',
    secret: '',
    removeSecret: false,
    scope: ALL_SCOPE,
    enabled: true,
  }
  if (!c) return base
  return {
    ...base,
    name: c.name,
    type: c.type,
    interval: String(c.interval_seconds),
    timeout: String(c.timeout_ms),
    retries: String(c.retries),
    host: c.target_host ?? '',
    port: txt(c.target_port),
    url: c.url ?? '',
    expectStatus: c.expect_status ?? '',
    expectText: c.expect_text ?? '',
    serviceName: c.service_name ?? '',
    warnMs: txt(c.warn_ms),
    critMs: txt(c.crit_ms),
    warnDays: c.warn_days === null ? base.warnDays : String(c.warn_days),
    critDays: c.crit_days === null ? base.critDays : String(c.crit_days),
    headerName: c.header_name ?? '',
    scope: scopeToDraft(c.scope),
    enabled: c.enabled,
  }
}

/** Entero dentro de [min, max]; null si vacío o inválido. */
function int(raw: string, min: number, max: number): number | null {
  const t = raw.trim()
  if (!/^\d+$/.test(t)) return null
  const n = Number(t)
  return n >= min && n <= max ? n : null
}

/** Aviso (no bloquea) si la query de la URL parece llevar credenciales; el servidor la rechaza con 400. */
export function urlQueryLooksSecret(raw: string): boolean {
  const i = raw.indexOf('?')
  if (i < 0) return false
  const keys = [...new URLSearchParams(raw.slice(i + 1).split('#')[0]).keys()].map((k) => k.toLowerCase())
  return keys.some((k) => SECRET_KEYS.some((s) => k.includes(s)))
}

function validateUrl(raw: string): string | undefined {
  const url = raw.trim()
  if (url.length < 8 || url.length > MAX_URL) return `La URL debe tener entre 8 y ${MAX_URL} caracteres.`
  if (/\s/.test(url) || [...url].some((ch) => ch.charCodeAt(0) < 32 || ch.charCodeAt(0) === 127)) return 'La URL no puede tener espacios ni caracteres de control.'
  let u: URL
  try {
    u = new URL(url)
  } catch {
    return 'Ingresa una URL válida: http:// o https:// con host.'
  }
  if (u.protocol !== 'http:' && u.protocol !== 'https:') return 'La URL debe ser http:// o https://.'
  if (u.username || u.password) return 'La URL no puede llevar usuario ni clave: usa el campo secreto.'
  if (url.includes('#')) return 'La URL no puede llevar fragmento (#).'
  if (urlQueryLooksSecret(url)) return 'La query de la URL no puede llevar tokens ni credenciales: usa el campo secreto.'
  return undefined
}

function validateStatus(raw: string): string | undefined {
  const m = STATUS_RE.exec(raw.trim())
  const lo = m ? Number(m[1]) : 0
  const hi = m?.[2] ? Number(m[2]) : lo
  if (!m || lo < 100 || hi > 599 || lo > hi) return 'Un código ("200") o un rango ("200-399") entre 100 y 599.'
  return undefined
}

/** Valida el borrador con las mismas reglas del servidor y arma el cuerpo (crear: completo; editar: PATCH sin `type`). */
export function buildCheckInput(d: CheckDraft, original: ServiceCheck | null): { input: CheckInput } | { errors: CheckErrors } {
  const editing = original !== null
  const errors: CheckErrors = {}
  const name = d.name.trim()
  if (!name) errors.name = 'Ingresa un nombre.'
  else if (name.length > MAX_NAME) errors.name = `Máximo ${MAX_NAME} caracteres.`

  const interval = int(d.interval, MIN_INTERVAL, MAX_INTERVAL)
  if (interval === null) errors.interval = `Intervalo: entre ${MIN_INTERVAL} y ${MAX_INTERVAL.toLocaleString('es-PE')} segundos.`
  const timeout = int(d.timeout, MIN_TIMEOUT, MAX_TIMEOUT)
  if (timeout === null) errors.timeout = `Timeout: entre ${MIN_TIMEOUT} y ${MAX_TIMEOUT.toLocaleString('es-PE')} ms.`
  else if (interval !== null && timeout > interval * 1000) errors.timeout = 'El timeout no puede superar el intervalo.'
  const retries = int(d.retries, 0, MAX_RETRIES)
  if (retries === null) errors.retries = `Reintentos: entre 0 y ${MAX_RETRIES}.`
  const scope = draftToScope(d.scope)
  if ('error' in scope) errors.scope = scope.error

  const input: CheckInput = { name, enabled: d.enabled }
  if (interval !== null) input.interval_seconds = interval
  if (timeout !== null) input.timeout_ms = timeout
  if (retries !== null) input.retries = retries
  if ('scope' in scope) input.scope = scope.scope
  if (!editing) input.type = d.type

  const hostPort = () => {
    const host = d.host.trim()
    if (!host || host.length > 253 || !HOST_RE.test(host)) errors.host = 'Host: 1 a 253 caracteres A-Z a-z 0-9 . _ : -'
    else input.target_host = host
    const port = int(d.port, 1, 65535)
    if (port === null) errors.port = 'Puerto: entre 1 y 65535.'
    else input.target_port = port
  }
  // Latencia opcional: vacío = sin umbral (al editar se envía null para quitarlo).
  const latency = () => {
    const w = d.warnMs.trim() === '' ? undefined : int(d.warnMs, 1, MAX_LATENCY)
    const c = d.critMs.trim() === '' ? undefined : int(d.critMs, 1, MAX_LATENCY)
    if (w === null) errors.warnMs = `Advertencia: entre 1 y ${MAX_LATENCY.toLocaleString('es-PE')} ms.`
    if (c === null) errors.critMs = `Crítico: entre 1 y ${MAX_LATENCY.toLocaleString('es-PE')} ms.`
    if (w && c && w >= c) errors.warnMs = 'La advertencia debe ser menor que el umbral crítico.'
    if (w != null) input.warn_ms = w
    else if (editing) input.warn_ms = null
    if (c != null) input.crit_ms = c
    else if (editing) input.crit_ms = null
  }

  switch (d.type) {
    case 'tcp':
      hostPort()
      latency()
      break
    case 'http': {
      const urlError = validateUrl(d.url)
      if (urlError) errors.url = urlError
      else input.url = d.url.trim()
      const status = d.expectStatus.trim()
      if (status) {
        const s = validateStatus(status)
        if (s) errors.expectStatus = s
        else input.expect_status = status
      } else if (editing) input.expect_status = null
      const text = d.expectText
      if (text.length > MAX_EXPECT_TEXT) errors.expectText = `Máximo ${MAX_EXPECT_TEXT} caracteres.`
      else if (text !== '') input.expect_text = text
      else if (editing) input.expect_text = null
      latency()
      buildSecret(d, original, input, errors)
      break
    }
    case 'tls_cert': {
      hostPort()
      const warn = int(d.warnDays, 1, MAX_DAYS)
      const crit = int(d.critDays, 0, MAX_DAYS)
      if (warn === null) errors.warnDays = `Advertencia: entre 1 y ${MAX_DAYS} días.`
      if (crit === null) errors.critDays = `Crítico: entre 0 y ${MAX_DAYS} días.`
      if (warn !== null && crit !== null && crit >= warn) errors.critDays = 'Los días críticos deben ser menos que los de advertencia.'
      if (warn !== null) input.warn_days = warn
      if (crit !== null) input.crit_days = crit
      break
    }
    case 'service': {
      const svc = d.serviceName.trim()
      if (!SERVICE_RE.test(svc)) errors.serviceName = 'Servicio: 1 a 64 caracteres A-Z a-z 0-9 @ . _ : -'
      else input.service_name = svc
      break
    }
  }
  return Object.keys(errors).length > 0 ? { errors } : { input }
}

/** Cabecera + secreto: el secreto es solo de escritura; vacío al editar = conservar; "Quitar secreto" = null. */
function buildSecret(d: CheckDraft, original: ServiceCheck | null, input: CheckInput, errors: CheckErrors) {
  const hasSecret = original?.has_secret ?? false
  if (hasSecret && d.removeSecret) {
    input.secret = null
    return
  }
  const secret = d.secret
  if (!d.headerName && !secret && !hasSecret) return
  if (!d.headerName) {
    errors.headerName = 'Elige la cabecera donde se envía el secreto.'
    return
  }
  if (!secret && !hasSecret) {
    errors.secret = 'Ingresa el secreto de la cabecera (o quita la cabecera).'
    return
  }
  if (secret.length > MAX_SECRET) {
    errors.secret = `Máximo ${MAX_SECRET} caracteres.`
    return
  }
  input.header_name = d.headerName
  if (secret) input.secret = secret
}
