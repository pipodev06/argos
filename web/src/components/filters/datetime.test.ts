import { describe, expect, it } from 'vitest'
import { formatDateTimeSummary, fromLocalInput, toLocalInput } from './datetime'

describe('datetime', () => {
  it('fromLocalInput da RFC 3339 UTC sin milisegundos', () => {
    const out = fromLocalInput('2026-09-24T10:30')
    expect(out).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:00Z$/)
    expect(new Date(out).getTime()).toBe(new Date(2026, 8, 24, 10, 30).getTime())
  })
  it('ida y vuelta local -> UTC -> local', () => {
    expect(toLocalInput(fromLocalInput('2026-01-05T08:05'))).toBe('2026-01-05T08:05')
  })
  it('entradas inválidas dan vacío', () => {
    expect(fromLocalInput('')).toBe('')
    expect(toLocalInput('basura')).toBe('')
  })
  it('resumen en hora local', () => {
    expect(formatDateTimeSummary(new Date(2026, 8, 4, 9, 5).toISOString())).toBe('04/09/2026 09:05')
  })
})
