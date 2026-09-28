// @vitest-environment jsdom
import { act, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { ApiError } from '@/lib/api'
import type { GroupPurgeResult, PurgeJob } from '../types/purge.types'
import { groupResultOf, purgeProgressLabel } from '../utils/purge'
import { usePurgeJob } from './usePurgeJob'

const getPurgeJob = vi.fn()
vi.mock('../services/purge.service', () => ({ getPurgeJob: (...a: unknown[]) => getPurgeJob(...a) }))

const job = (over: Partial<PurgeJob> = {}): PurgeJob => ({
  id: 'j1', kind: 'group', target_name: 'Acme', state: 'queued', total_hosts: 10, done_hosts: 0, error: null,
  created_at: '2026-09-28T10:00:00Z', started_at: null, finished_at: null, result: null, ...over,
})

beforeEach(() => {
  vi.useFakeTimers()
  getPurgeJob.mockReset()
})
afterEach(() => vi.useRealTimers())

describe('usePurgeJob', () => {
  it('sondea cada 2 s hasta done y luego se detiene', async () => {
    getPurgeJob
      .mockResolvedValueOnce(job({ state: 'running', done_hosts: 3 }))
      .mockResolvedValueOnce(job({ state: 'done', done_hosts: 10, result: {} }))
    const { result } = renderHook(() => usePurgeJob(job()))
    expect(getPurgeJob).not.toHaveBeenCalled()
    await act(() => vi.advanceTimersByTimeAsync(2000))
    expect(result.current.job?.done_hosts).toBe(3)
    await act(() => vi.advanceTimersByTimeAsync(2000))
    expect(result.current.job?.state).toBe('done')
    await act(() => vi.advanceTimersByTimeAsync(10000))
    expect(getPurgeJob).toHaveBeenCalledTimes(2)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('se detiene en failed', async () => {
    getPurgeJob.mockResolvedValueOnce(job({ state: 'failed', error: 'la baja falló; reintenta' }))
    const { result } = renderHook(() => usePurgeJob(job()))
    await act(() => vi.advanceTimersByTimeAsync(2000))
    expect(result.current.job?.state).toBe('failed')
    await act(() => vi.advanceTimersByTimeAsync(10000))
    expect(getPurgeJob).toHaveBeenCalledTimes(1)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('reintenta tras un error de red transitorio y para en 404', async () => {
    getPurgeJob
      .mockRejectedValueOnce(new ApiError('network_error', 'sin red'))
      .mockResolvedValueOnce(job({ state: 'running', done_hosts: 1 }))
      .mockRejectedValueOnce(new ApiError('not_found', 'no existe', 404))
    const { result } = renderHook(() => usePurgeJob(job()))
    await act(() => vi.advanceTimersByTimeAsync(2000))
    expect(result.current.error).toBeNull()
    await act(() => vi.advanceTimersByTimeAsync(2000))
    expect(result.current.job?.done_hosts).toBe(1)
    await act(() => vi.advanceTimersByTimeAsync(2000))
    expect(result.current.error).toBe('no existe')
    expect(vi.getTimerCount()).toBe(0)
  })

  it('no consulta si el trabajo ya llegó terminado y limpia el timer al desmontar', async () => {
    renderHook(() => usePurgeJob(job({ state: 'done', result: {} })))
    await act(() => vi.advanceTimersByTimeAsync(5000))
    expect(getPurgeJob).not.toHaveBeenCalled()
    getPurgeJob.mockResolvedValue(job({ state: 'running' }))
    const { unmount } = renderHook(() => usePurgeJob(job()))
    unmount()
    expect(vi.getTimerCount()).toBe(0)
  })
})

describe('resultado y progreso', () => {
  const result = { agents: 1 } as GroupPurgeResult
  it('la constancia (resultado del grupo) solo existe con state done', () => {
    expect(groupResultOf(job({ state: 'done', result }))).toBe(result)
    expect(groupResultOf(job({ state: 'running', result }))).toBeNull()
    expect(groupResultOf(job({ state: 'failed', result: null }))).toBeNull()
    expect(groupResultOf(job({ state: 'done', kind: 'host', result }))).toBeNull()
  })
  it('etiqueta de progreso', () => {
    expect(purgeProgressLabel(job({ state: 'running', done_hosts: 3 }))).toBe('Borrando 3 de 10 equipos')
    expect(purgeProgressLabel(job({ kind: 'host', state: 'running', total_hosts: 1 }))).toBe('Borrando…')
    expect(purgeProgressLabel(job())).toBe('En cola…')
  })
})
