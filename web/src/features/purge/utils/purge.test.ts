import { describe, expect, it } from 'vitest'
import { ApiError } from '@/lib/api'
import { mapPurgeGroupError, mapPurgeHostError, purgeNameMatches, summarizeGroupPurge } from './purge'

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
      failed_host_ids: [], rules: 1, windows: 0, checks: 2, enroll_tokens: 0, users_disabled: 1, users_scope_reduced: 0, group_deleted: true,
    })
    expect(lines[0]).toMatch(/2$/)
    expect(lines.join('\n')).toMatch(/El grupo se eliminó/)
  })
})
