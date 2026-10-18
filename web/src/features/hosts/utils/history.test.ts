import { describe, expect, it } from 'vitest'
import {
  csvFilename,
  expectedResolution,
  filenameFromDisposition,
  parseLocalInput,
  shortcutRange,
  tickStepFor,
  toLocalInput,
  toUtcIso,
  validateRange,
} from './history'

const DAY = 86_400_000
const NOW = new Date(2026, 8, 26, 15, 30).getTime() // 26 sep 2026 15:30 local

describe('conversion local <-> UTC', () => {
  it('ida y vuelta de datetime-local', () => {
    expect(parseLocalInput('2026-09-25T14:05')).toBe(new Date(2026, 8, 25, 14, 5).getTime())
    expect(toLocalInput(new Date(2026, 8, 25, 14, 5).getTime())).toBe('2026-09-25T14:05')
  })
  it('vacio es NaN y UTC es ISO con Z', () => {
    expect(parseLocalInput('')).toBeNaN()
    expect(toUtcIso(Date.UTC(2026, 8, 25, 19, 5))).toBe('2026-09-25T19:05:00.000Z')
  })
})

describe('atajos', () => {
  it('ayer: 00:00 a 24:00 local', () => {
    expect(shortcutRange('yesterday', NOW)).toEqual({
      fromMs: new Date(2026, 8, 25).getTime(),
      toMs: new Date(2026, 8, 26).getTime(),
    })
  })
  it('hace 7 dias termina ahora', () => {
    expect(shortcutRange('last7', NOW)).toEqual({ fromMs: NOW - 7 * DAY, toMs: NOW })
  })
  it('este mes y mes pasado (cruzando el ano)', () => {
    expect(shortcutRange('thisMonth', NOW).fromMs).toBe(new Date(2026, 8, 1).getTime())
    const jan = new Date(2026, 0, 10).getTime()
    expect(shortcutRange('lastMonth', jan)).toEqual({
      fromMs: new Date(2025, 11, 1).getTime(),
      toMs: new Date(2026, 0, 1).getTime(),
    })
  })
})

describe('resolucion y topes', () => {
  it('resolucion por antiguedad', () => {
    expect(expectedResolution(NOW - 29 * DAY, NOW)).toBe('raw')
    expect(expectedResolution(NOW - 31 * DAY, NOW)).toBe('hourly')
    // Un host con 90 días de detalle (retención por suscripción) sirve crudo hasta hace 90 días.
    expect(expectedResolution(NOW - 60 * DAY, NOW, 90)).toBe('raw')
    expect(expectedResolution(NOW - 91 * DAY, NOW, 90)).toBe('hourly')
  })
  it('rango valido', () => {
    expect(validateRange({ fromMs: NOW - DAY, toMs: NOW }, NOW)).toBeNull()
  })
  it('fin antes del inicio, vacio y futuro', () => {
    expect(validateRange({ fromMs: NOW, toMs: NOW - 1 }, NOW)).toMatch(/posterior/)
    expect(validateRange({ fromMs: NaN, toMs: NOW }, NOW)).toMatch(/Indica/)
    expect(validateRange({ fromMs: NOW - DAY, toMs: NOW + 10 * 60_000 }, NOW)).toMatch(/futuro/)
  })
  it('tope 366 d por hora; desde hace 29 d hasta ahora es valido', () => {
    expect(validateRange({ fromMs: NOW - 29 * DAY, toMs: NOW }, NOW)).toBeNull()
    expect(validateRange({ fromMs: NOW - 400 * DAY, toMs: NOW - 30 * DAY }, NOW)).toMatch(/366/)
    expect(validateRange({ fromMs: NOW - 366 * DAY, toMs: NOW }, NOW)).toBeNull()
  })
})

describe('archivo y ejes', () => {
  it('nombre por defecto', () => {
    const range = { fromMs: new Date(2026, 6, 20).getTime(), toMs: new Date(2026, 6, 21).getTime() }
    expect(csvFilename('metrics', 'srv web/1', range)).toBe('metricas-srv_web_1-2026-07-20_2026-07-21.csv')
    expect(csvFilename('disks', 'a', range)).toBe('discos-a-2026-07-20_2026-07-21.csv')
  })
  it('Content-Disposition', () => {
    expect(filenameFromDisposition('attachment; filename="metricas-a-b.csv"')).toBe('metricas-a-b.csv')
    expect(filenameFromDisposition('attachment; filename="../x.csv"')).toBe('.._x.csv')
    expect(filenameFromDisposition(null)).toBeNull()
  })
  it('escalon de marcas', () => {
    expect(tickStepFor(DAY)).toBe(4 * 3_600_000)
    expect(tickStepFor(30 * DAY)).toBe(5 * DAY)
  })
})
