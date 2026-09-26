import { describe, expect, it } from 'vitest'
import { ApiError } from '@/lib/api'
import { estimateWeight, EST_BYTES_PER_HOST_DAY, formatBytes, parseDetailDays, parseShrink, sourceLabel } from './retention'

describe('parseDetailDays', () => {
  it('acepta el rango 8..730', () => {
    expect(parseDetailDays(' 8 ')).toEqual({ days: 8 })
    expect(parseDetailDays('730')).toEqual({ days: 730 })
  })
  it('rechaza fuera de rango, decimales, vacío y texto', () => {
    for (const raw of ['7', '731', '', '10.5', 'abc', '-9', '1e2']) expect('error' in parseDetailDays(raw)).toBe(true)
  })
})

describe('estimateWeight', () => {
  it('90 días x 23 KB = 2070 KB por host; con 50 hosts', () => {
    const w = estimateWeight(90, 50)
    expect(w.perHost).toBe(90 * EST_BYTES_PER_HOST_DAY)
    expect(w.total).toBe(50 * 90 * 23 * 1024)
  })
  it('sin hosts, total 0', () => expect(estimateWeight(30, 0).total).toBe(0))
})

describe('formatBytes', () => {
  it('escala las unidades', () => {
    expect(formatBytes(512)).toBe('512 B')
    expect(formatBytes(2070 * 1024)).toMatch(/^2,0 MB$|^2 MB$|^2\.0 MB$/)
    expect(formatBytes(5 * 1024 ** 3)).toMatch(/^5(,0|\.0)? GB$/)
  })
})

describe('parseShrink', () => {
  it('lee los detalles del 409', () => {
    const err = new ApiError('shrink_needs_confirm', 'x', 409, undefined, { days_lost: 60, chunks: 120, est_bytes: 2_764_800 })
    expect(parseShrink(err)).toEqual({ days_lost: 60, chunks: 120, est_bytes: 2_764_800 })
  })
  it('otros errores no son confirmación', () => {
    expect(parseShrink(new ApiError('bad_request', 'x', 400))).toBeNull()
    expect(parseShrink(new Error('x'))).toBeNull()
  })
})

describe('sourceLabel', () => {
  it('traduce los orígenes conocidos y deja los demás', () => {
    expect(sourceLabel('group')).toBe('Heredada del grupo')
    expect(sourceLabel('zzz')).toBe('zzz')
  })
})
