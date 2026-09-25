import { describe, expect, it } from 'vitest'
import { rowNumber } from './pagination'

describe('rowNumber', () => {
  it('numera desde 1 en la primera pagina', () => {
    expect(rowNumber(1, 15, 0)).toBe(1)
    expect(rowNumber(1, 15, 14)).toBe(15)
  })
  it('es continuo entre paginas', () => {
    expect(rowNumber(2, 15, 0)).toBe(16)
    expect(rowNumber(3, 50, 4)).toBe(105)
  })
})
