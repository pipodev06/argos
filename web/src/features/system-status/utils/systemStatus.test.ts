import { describe, expect, it } from 'vitest'
import { checkLabel, formatDuration } from './systemStatus'

describe('systemStatus utils', () => {
  it('etiqueta los cuatro checks en español y deja pasar uno desconocido', () => {
    expect(checkLabel('maintenance')).toBe('Mantenimiento')
    expect(checkLabel('evaluator')).toBe('Evaluador de alertas')
    expect(checkLabel('notification_queue')).toBe('Cola de notificaciones')
    expect(checkLabel('notification_dead')).toBe('Entregas fallidas')
    expect(checkLabel('otro')).toBe('otro')
  })

  it('formatea duraciones con dos unidades', () => {
    expect(formatDuration(45)).toBe('45 s')
    expect(formatDuration(300)).toBe('5 min')
    expect(formatDuration(7500)).toBe('2 h 5 min')
    expect(formatDuration(3 * 86400 + 4 * 3600 + 59)).toBe('3 d 4 h')
    expect(formatDuration(86400)).toBe('1 d')
    expect(formatDuration(-5)).toBe('0 s')
  })
})
