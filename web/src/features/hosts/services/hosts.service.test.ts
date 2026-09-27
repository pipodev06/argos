import { afterEach, describe, expect, it, vi } from 'vitest'
import { mockApi } from '@/test/utils'
import { reEnrollHost, revokeHost } from './hosts.service'

afterEach(() => vi.unstubAllGlobals())

describe('reEnrollHost', () => {
  it('llama POST /hosts/{id}/re-enroll sin body', async () => {
    const calls = mockApi({
      'POST /hosts/h1/re-enroll': { data: { enroll_token: 'tok', expires_at: '2026-09-28T00:00:00Z' } },
    })
    const result = await reEnrollHost('h1')
    expect(calls).toEqual([{ method: 'POST', path: '/hosts/h1/re-enroll', body: undefined }])
    expect(result.enroll_token).toBe('tok')
  })
})

describe('revokeHost', () => {
  it('llama POST /hosts/{id}/revoke sin body', async () => {
    const calls = mockApi({ 'POST /hosts/h1/revoke': { data: null } })
    await revokeHost('h1')
    expect(calls).toEqual([{ method: 'POST', path: '/hosts/h1/revoke', body: undefined }])
  })
})
