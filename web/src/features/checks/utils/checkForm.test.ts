import { describe, expect, it } from 'vitest'
import type { ServiceCheck } from '../types/check.types'
import { buildCheckInput, checkToDraft, urlQueryLooksSecret } from './checkForm'

const base = { ...checkToDraft(null), name: 'Chequeo' }
const errorsOf = (r: ReturnType<typeof buildCheckInput>) => ('errors' in r ? r.errors : {})

describe('chequeos: comunes', () => {
  it('borrador por defecto: TCP cada 60 s, timeout 5000 ms, 1 reintento', () => {
    expect(checkToDraft(null)).toMatchObject({ type: 'tcp', interval: '60', timeout: '5000', retries: '1', enabled: true })
  })
  it('nombre, intervalo (30-86400 s), timeout (200-30000 ms) y reintentos (0-3)', () => {
    const tcp = { ...base, host: 'a.example.com', port: '443' }
    expect(errorsOf(buildCheckInput({ ...tcp, name: '' }, null))).toHaveProperty('name')
    expect(errorsOf(buildCheckInput({ ...tcp, interval: '29' }, null))).toHaveProperty('interval')
    expect(errorsOf(buildCheckInput({ ...tcp, interval: '86401' }, null))).toHaveProperty('interval')
    expect(errorsOf(buildCheckInput({ ...tcp, timeout: '199' }, null))).toHaveProperty('timeout')
    expect(errorsOf(buildCheckInput({ ...tcp, retries: '4' }, null))).toHaveProperty('retries')
    expect(errorsOf(buildCheckInput({ ...tcp, retries: '-1' }, null))).toHaveProperty('retries')
  })
  it('el timeout no puede superar el intervalo', () => {
    expect(errorsOf(buildCheckInput({ ...base, host: 'a.b', port: '80', interval: '30', timeout: '31000' }, null))).toHaveProperty('timeout')
  })
  it('al crear envía el tipo; al editar (PATCH) no', () => {
    const d = { ...base, host: 'a.b', port: '80' }
    expect(buildCheckInput(d, null)).toMatchObject({ input: { type: 'tcp' } })
    const original = { id: 'c1', has_secret: false } as unknown as ServiceCheck
    const r = buildCheckInput(d, original)
    expect(r).toHaveProperty('input')
    if ('input' in r) expect(r.input).not.toHaveProperty('type')
  })
})

describe('chequeo TCP', () => {
  it('host y puerto válidos', () => {
    expect(buildCheckInput({ ...base, host: 'db.internal', port: '5432' }, null)).toMatchObject({ input: { target_host: 'db.internal', target_port: 5432 } })
  })
  it.each([['0'], ['65536'], ['80.5'], ['']])('rechaza el puerto %s', (port) => {
    expect(errorsOf(buildCheckInput({ ...base, host: 'a.b', port }, null))).toHaveProperty('port')
  })
  it.each([[''], ['con espacio'], ['a/b'], ['x'.repeat(254)]])('rechaza el host "%s"', (host) => {
    expect(errorsOf(buildCheckInput({ ...base, host: host.slice(0, 260), port: '80' }, null))).toHaveProperty('host')
  })
  it('latencia: advertencia menor que crítico; al editar vacío = null (la quita)', () => {
    const d = { ...base, host: 'a.b', port: '80' }
    expect(errorsOf(buildCheckInput({ ...d, warnMs: '500', critMs: '100' }, null))).toHaveProperty('warnMs')
    expect(buildCheckInput({ ...d, warnMs: '100', critMs: '500' }, null)).toMatchObject({ input: { warn_ms: 100, crit_ms: 500 } })
    const original = { id: 'c1', has_secret: false } as unknown as ServiceCheck
    expect(buildCheckInput(d, original)).toMatchObject({ input: { warn_ms: null, crit_ms: null } })
  })
})

describe('chequeo HTTP', () => {
  const http = { ...base, type: 'http' as const }
  it('URL válida, código esperado y texto', () => {
    expect(buildCheckInput({ ...http, url: 'https://ejemplo.com/health', expectStatus: '200-399', expectText: 'ok' }, null)).toMatchObject({
      input: { url: 'https://ejemplo.com/health', expect_status: '200-399', expect_text: 'ok' },
    })
  })
  it.each([
    ['corta', 'http://'], ['sin http', 'ftp://ejemplo.com'], ['con usuario', 'https://user:pass@ejemplo.com'],
    ['con fragmento', 'https://ejemplo.com/#x'], ['con espacios', 'https://ejemplo.com/a b'], ['con token', 'https://ejemplo.com/?api_key=1'],
  ])('rechaza la URL %s', (_n, url) => {
    expect(errorsOf(buildCheckInput({ ...http, url }, null))).toHaveProperty('url')
  })
  it.each([['99'], ['600'], ['300-200'], ['abc']])('rechaza el código esperado %s', (expectStatus) => {
    expect(errorsOf(buildCheckInput({ ...http, url: 'https://ejemplo.com', expectStatus }, null))).toHaveProperty('expectStatus')
  })
  it('cabecera y secreto van juntos', () => {
    const d = { ...http, url: 'https://ejemplo.com' }
    expect(errorsOf(buildCheckInput({ ...d, secret: 's3' }, null))).toHaveProperty('headerName')
    expect(errorsOf(buildCheckInput({ ...d, headerName: 'Authorization' }, null))).toHaveProperty('secret')
    expect(buildCheckInput({ ...d, headerName: 'Authorization', secret: 's3' }, null)).toMatchObject({ input: { header_name: 'Authorization', secret: 's3' } })
  })
  it('al editar: secreto vacío conserva el guardado; "quitar secreto" envía null', () => {
    const d = { ...http, url: 'https://ejemplo.com', headerName: 'Authorization' }
    const original = { id: 'c1', has_secret: true } as unknown as ServiceCheck
    const keep = buildCheckInput(d, original)
    expect(keep).toHaveProperty('input.header_name', 'Authorization')
    if ('input' in keep) expect(keep.input).not.toHaveProperty('secret')
    expect(buildCheckInput({ ...d, removeSecret: true }, original)).toMatchObject({ input: { secret: null } })
  })
  it('detecta credenciales en la query de la URL', () => {
    expect(urlQueryLooksSecret('https://a.b/?token=1')).toBe(true)
    expect(urlQueryLooksSecret('https://a.b/?page=2')).toBe(false)
    expect(urlQueryLooksSecret('https://a.b/')).toBe(false)
  })
})

describe('chequeo de certificado TLS', () => {
  const tls = { ...base, type: 'tls_cert' as const, host: 'a.example.com', port: '443' }
  it('días de advertencia (por defecto 30) y críticos (7)', () => {
    expect(buildCheckInput(tls, null)).toMatchObject({ input: { warn_days: 30, crit_days: 7, target_host: 'a.example.com', target_port: 443 } })
  })
  it('crítico debe ser menor que advertencia; rangos', () => {
    expect(errorsOf(buildCheckInput({ ...tls, warnDays: '10', critDays: '10' }, null))).toHaveProperty('critDays')
    expect(errorsOf(buildCheckInput({ ...tls, warnDays: '0' }, null))).toHaveProperty('warnDays')
    expect(errorsOf(buildCheckInput({ ...tls, warnDays: '366' }, null))).toHaveProperty('warnDays')
    expect(buildCheckInput({ ...tls, warnDays: '10', critDays: '0' }, null)).toMatchObject({ input: { warn_days: 10, crit_days: 0 } })
  })
})

describe('chequeo de servicio', () => {
  it('nombre de servicio válido; rechaza vacío y caracteres raros', () => {
    const svc = { ...base, type: 'service' as const }
    expect(buildCheckInput({ ...svc, serviceName: 'ssh.service' }, null)).toMatchObject({ input: { service_name: 'ssh.service' } })
    expect(errorsOf(buildCheckInput({ ...svc, serviceName: '' }, null))).toHaveProperty('serviceName')
    expect(errorsOf(buildCheckInput({ ...svc, serviceName: 'a b; rm' }, null))).toHaveProperty('serviceName')
  })
})
