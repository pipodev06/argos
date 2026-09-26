import { describe, expect, it } from 'vitest'
import { canAckAlerts, canResolveAlerts } from './permissions'

describe('permisos de alertas', () => {
  it('reconocer: admin o can_ack', () => {
    expect(canAckAlerts({ role: 'admin', can_ack: false })).toBe(true)
    expect(canAckAlerts({ role: 'viewer', can_ack: true })).toBe(true)
    expect(canAckAlerts({ role: 'viewer', can_ack: false })).toBe(false)
    expect(canAckAlerts(null)).toBe(false)
  })
  it('resolver: solo admin', () => {
    expect(canResolveAlerts({ role: 'viewer', can_ack: true })).toBe(false)
    expect(canResolveAlerts({ role: 'admin', can_ack: false })).toBe(true)
  })
})
