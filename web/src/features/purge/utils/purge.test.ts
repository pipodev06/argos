import { describe, expect, it } from 'vitest'
import { ApiError } from '@/lib/api'
import type { GroupPurgeResult } from '../types/purge.types'
import { buildConstancia, constanciaFilename, mapPurgeGroupError, mapPurgeHostError, purgeNameMatches, summarizeGroupPurge } from './purge'

describe('purgeNameMatches', () => {
  it('exige el nombre exacto (sin recortar ni ignorar mayúsculas)', () => {
    expect(purgeNameMatches('srv-01', 'srv-01')).toBe(true)
    expect(purgeNameMatches('SRV-01', 'srv-01')).toBe(false)
    expect(purgeNameMatches('srv-01 ', 'srv-01')).toBe(false)
    expect(purgeNameMatches('', '')).toBe(false)
  })
})

describe('mapPurgeHostError', () => {
  it('409 colector: dice cuántos dispositivos hay', () => {
    const e = new ApiError('collector_has_devices', 'x', 409, undefined, { devices: 3 })
    expect(mapPurgeHostError(e)).toMatch(/3 dispositivos/)
  })
  it('nombre distinto y errores desconocidos', () => {
    expect(mapPurgeHostError(new ApiError('confirm_mismatch', 'x', 400))).toMatch(/no coincide/)
    expect(mapPurgeHostError(new ApiError('otro', 'x', 500))).toBeNull()
  })
})

describe('mapPurgeGroupError', () => {
  it('purga en curso', () => {
    expect(mapPurgeGroupError(new ApiError('purge_in_progress', 'x', 409))).toMatch(/en curso/)
  })
})

describe('summarizeGroupPurge', () => {
  it('resume las cuentas del resultado', () => {
    const lines = summarizeGroupPurge({
      agents: 2, devices: 1, deleted_rows: { metrics: 1000, metrics_hourly: 5, metrics_chunks: 4, disk_samples: 0, service_checks: 0 },
      failed_host_ids: [], rules: 1, windows: 0, checks: 2, enroll_tokens: 0, channels: 0, snmp_profiles: 0, users_disabled: 1, users_scope_reduced: 0, group_deleted: true,
      group_name: 'Acme', purged_at: '2026-09-27T15:00:00Z', performed_by: 'admin',
    })
    expect(lines[0]).toMatch(/2$/)
    expect(lines.join('\n')).toMatch(/El grupo se eliminó/)
  })
})

describe('constancia de baja', () => {
  const base: GroupPurgeResult = {
    agents: 3, devices: 2, deleted_rows: { metrics: 1000, metrics_hourly: 5, metrics_chunks: 4, disk_samples: 0, service_checks: 0 },
    failed_host_ids: ['h-secreto-1', 'h-secreto-2'], rules: 1, windows: 2, checks: 4, enroll_tokens: 5, channels: 6, snmp_profiles: 7,
    users_disabled: 8, users_scope_reduced: 9, group_deleted: false,
    group_name: 'Acme Perú', purged_at: '2026-09-27T15:00:00Z', performed_by: 'admin',
  }
  it('incluye cliente, ejecutor y conteos, sin ids de hosts', () => {
    const t = buildConstancia(base)
    expect(t).toMatch(/Cliente: Acme Perú/)
    expect(t).toMatch(/Ejecutado por: admin/)
    expect(t).toMatch(/Hosts con agente eliminados: 3/)
    expect(t).toMatch(/Perfiles SNMP: 7/)
    expect(t).toMatch(/Hosts que fallaron y no se purgaron: 2/)
    expect(t).not.toMatch(/h-secreto/)
  })
  it('omite la línea de fallidos si no hubo', () => {
    expect(buildConstancia({ ...base, failed_host_ids: [] })).not.toMatch(/fallaron/)
  })
  it('sanea el nombre del archivo', () => {
    expect(constanciaFilename('Acme Perú / S.A.', '2026-09-27T15:00:00Z')).toMatch(/^constancia-baja-acme-peru-s-a-2026092[678]\.txt$/)
  })
})
