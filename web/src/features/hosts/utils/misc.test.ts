import { describe, expect, it } from 'vitest'
import { OPTIONAL_COLUMNS } from './columns'
import { formatBps, formatCapacity, formatElapsed, formatLastSeen, formatMemory, formatNumber } from './format'
import { isHostId } from './id'
import { formatOs } from './os'
import { hostStatusDetail } from './status'

const NOW = Date.parse('2026-09-24T12:00:00Z')
const ago = (s: number) => new Date(NOW - s * 1000).toISOString()

describe('columns', () => {
  it('ids únicos', () => {
    const ids = OPTIONAL_COLUMNS.map((c) => c.id)
    expect(new Set(ids).size).toBe(ids.length)
  })
})

describe('isHostId', () => {
  it('acepta UUID y rechaza el resto', () => {
    expect(isHostId('123e4567-e89b-12d3-a456-426614174000')).toBe(true)
    expect(isHostId('123E4567-E89B-12D3-A456-426614174000')).toBe(true)
    expect(isHostId('123e4567')).toBe(false)
    expect(isHostId('')).toBe(false)
    expect(isHostId('123e4567-e89b-12d3-a456-42661417400012')).toBe(false)
  })
})

describe('formatOs', () => {
  it('quita "Microsoft " y agrega versión si aporta', () => {
    expect(formatOs('windows', 'Microsoft Windows 11 Pro', '10.0.26200')).toBe('Windows 11 Pro 10.0.26200')
    expect(formatOs('linux', 'Ubuntu', '24.04')).toBe('Ubuntu 24.04')
    expect(formatOs('linux', 'Ubuntu 24.04', '24.04')).toBe('Ubuntu 24.04')
  })
  it('sin nombre mapea el SO', () => {
    expect(formatOs('darwin')).toBe('macOS')
    expect(formatOs('Windows', null)).toBe('Windows')
    expect(formatOs('freebsd', '  ')).toBe('freebsd')
  })
})

describe('format', () => {
  it('tiempo transcurrido compacto', () => {
    expect(formatLastSeen(null, NOW)).toBe('nunca')
    expect(formatLastSeen(ago(30), NOW)).toBe('hace 30s')
    expect(formatLastSeen(ago(300), NOW)).toBe('hace 5min')
    expect(formatLastSeen(ago(3 * 3600), NOW)).toBe('hace 3h')
    expect(formatLastSeen(ago(2 * 86400), NOW)).toBe('hace 2d')
    expect(formatElapsed(ago(300), NOW)).toBe('5 min')
  })
  it('fechas futuras no dan negativos', () => {
    expect(formatLastSeen(ago(-100), NOW)).toBe('hace 0s')
  })
  it('memoria, capacidad y tasa', () => {
    expect(formatMemory(512)).toBe('512 MB')
    expect(formatMemory(2048)).toContain('GB')
    expect(formatCapacity(2 * 1024 * 1024)).toContain('TB')
    expect(formatBps(850)).toBe('850 B/s')
    expect(formatBps(12_300)).toContain('KB/s')
    expect(formatBps(4_500_000)).toContain('MB/s')
    expect(formatBps(2_000_000_000)).toContain('GB/s')
  })
  it('formatNumber respeta los decimales máximos', () => {
    expect(formatNumber(3, 2)).toBe('3')
    expect(formatNumber(3.456, 0)).toBe('3')
  })
})

describe('hostStatusDetail', () => {
  it('describe cada estado', () => {
    expect(hostStatusDetail('up', null, NOW)).toContain('normalidad')
    expect(hostStatusDetail('down', ago(300), NOW)).toContain('Sin reportar hace 5 min.')
    expect(hostStatusDetail('offline', null, NOW)).toMatch(/^Sin reportar\. /)
    expect(hostStatusDetail('pending', null, NOW)).toContain('primer reporte')
    expect(hostStatusDetail('disabled', null, NOW)).toContain('revocado')
  })
})
