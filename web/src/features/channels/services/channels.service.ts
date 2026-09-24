import { apiRequest } from '@/lib/api'
import type { Channel, CreateChannelInput, TestResult, UpdateChannelInput } from '../types/channel.types'

const enc = encodeURIComponent

export async function listChannels(signal?: AbortSignal): Promise<Channel[]> {
  const data = await apiRequest<{ items: Channel[] }>('/channels', { signal })
  return data.items
}

export async function createChannel(input: CreateChannelInput): Promise<Channel> {
  const data = await apiRequest<{ channel: Channel }>('/channels', { method: 'POST', body: input })
  return data.channel
}

export async function updateChannel(id: string, input: UpdateChannelInput): Promise<Channel> {
  const data = await apiRequest<{ channel: Channel }>(`/channels/${enc(id)}`, { method: 'PATCH', body: input })
  return data.channel
}

export function deleteChannel(id: string): Promise<unknown> {
  return apiRequest<unknown>(`/channels/${enc(id)}`, { method: 'DELETE', noData: true })
}

export function testChannel(id: string): Promise<TestResult> {
  return apiRequest<TestResult>(`/channels/${enc(id)}/test`, { method: 'POST' })
}
