import { describe, it, expect } from 'vitest'
import { formatAvailabilityPercent, formatSeconds, formatCoveragePct } from './format'

describe('Report formatters', () => {
  describe('formatAvailabilityPercent', () => {
    it('should return "Sin datos" for null', () => {
      expect(formatAvailabilityPercent(null)).toBe('Sin datos')
    })

    it('should format 100% correctly', () => {
      expect(formatAvailabilityPercent(100)).toBe('100.0%')
    })

    it('should format 99.5% correctly', () => {
      expect(formatAvailabilityPercent(99.5)).toBe('99.5%')
    })

    it('should round to 1 decimal', () => {
      expect(formatAvailabilityPercent(99.99)).toBe('100.0%')
      expect(formatAvailabilityPercent(99.94)).toBe('99.9%')
    })

    it('should handle 0%', () => {
      expect(formatAvailabilityPercent(0)).toBe('0.0%')
    })
  })

  describe('formatSeconds', () => {
    it('should return "0 s" for 0 seconds', () => {
      expect(formatSeconds(0)).toBe('0 s')
    })

    it('should format seconds only', () => {
      expect(formatSeconds(30)).toBe('30 s')
      expect(formatSeconds(59)).toBe('59 s')
    })

    it('should format minutes and seconds', () => {
      expect(formatSeconds(60)).toBe('1 m')
      expect(formatSeconds(90)).toBe('1 m 30 s')
      expect(formatSeconds(125)).toBe('2 m 5 s')
    })

    it('should format hours and minutes', () => {
      expect(formatSeconds(3600)).toBe('1 h')
      expect(formatSeconds(3660)).toBe('1 h 1 m')
      expect(formatSeconds(7325)).toBe('2 h 2 m')
    })

    it('should format days, hours, and minutes', () => {
      expect(formatSeconds(86400)).toBe('1 d')
      expect(formatSeconds(90000)).toBe('1 d 1 h')
      expect(formatSeconds(93600)).toBe('1 d 2 h')
    })

    it('should truncate to 2 parts', () => {
      expect(formatSeconds(90061)).toBe('1 d 1 h')
    })

    it('should handle large numbers', () => {
      expect(formatSeconds(604800)).toBe('7 d') // 1 week
    })
  })

  describe('formatCoveragePct', () => {
    it('should format coverage percentage as integer', () => {
      expect(formatCoveragePct(100)).toBe('100%')
      expect(formatCoveragePct(95.5)).toBe('96%')
      expect(formatCoveragePct(50)).toBe('50%')
    })
  })
})
