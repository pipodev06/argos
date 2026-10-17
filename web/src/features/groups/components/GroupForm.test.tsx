// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { checkA11y, mockApi } from '@/test/utils'
import { GroupForm } from './GroupForm'
import type { Group } from '../types/group.types'

afterEach(() => vi.unstubAllGlobals())

function renderCreate(onSaved = () => {}) {
  render(<GroupForm group={null} defaultDownAfter={300} onCancel={() => {}} onSaved={onSaved} />)
}

const GROUP: Group = {
  id: 'g1',
  name: 'Cliente A',
  down_after_seconds: null,
  always_on: false,
  host_count: 3,
  detail_retention_days: null,
  effective_detail_days: 30,
  retention_source: 'global',
  quotas: { max_hosts: 10, max_devices: null, max_checks: null, max_users: null, max_rules: null, max_channels: null },
}

function renderEdit(onSaved = () => {}) {
  render(<GroupForm group={GROUP} defaultDownAfter={300} onCancel={() => {}} onSaved={onSaved} />)
}

describe('GroupForm: cuotas del cliente (B7)', () => {
  it('muestra los 6 campos precargados con el limite actual y el uso, solo al editar', async () => {
    mockApi({ 'GET /groups/g1/usage': { data: { hosts: { current: 3, limit: 10 }, devices: { current: 0, limit: null }, checks: { current: 0, limit: null }, users: { current: 0, limit: null }, rules: { current: 0, limit: null }, channels: { current: 0, limit: null } } } })
    renderEdit()
    const hosts = (await screen.findByLabelText('Hosts')) as HTMLInputElement
    expect(hosts.value).toBe('10')
    expect(await screen.findByText('3 de 10 en uso')).toBeTruthy()
    expect((await screen.findAllByText('0 en uso, sin límite')).length).toBe(5)
    expect(await checkA11y()).toHaveNoViolations()
  })

  it('no aparecen al crear (sin group)', () => {
    renderCreate()
    expect(screen.queryByText('Cuotas del cliente')).toBeNull()
  })

  it('guardar sin tocar nada manda los mismos limites (no los borra por accidente)', async () => {
    mockApi({ 'GET /groups/g1/usage': { data: { hosts: { current: 3, limit: 10 }, devices: { current: 0, limit: null }, checks: { current: 0, limit: null }, users: { current: 0, limit: null }, rules: { current: 0, limit: null }, channels: { current: 0, limit: null } } } })
    const calls = mockApi({ 'PATCH /groups/g1': { data: { group: GROUP } } })
    const onSaved = vi.fn()
    renderEdit(onSaved)
    await screen.findByLabelText('Hosts')
    fireEvent.click(screen.getByRole('button', { name: 'Guardar' }))
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    const body = calls.find((c) => c.method === 'PATCH')?.body as Record<string, unknown>
    expect(body).toMatchObject({ max_hosts: 10, max_devices: null, max_checks: null, max_users: null, max_rules: null, max_channels: null })
  })

  it('un valor invalido (negativo) no llama al servidor', async () => {
    mockApi({ 'GET /groups/g1/usage': { data: { hosts: { current: 3, limit: 10 }, devices: { current: 0, limit: null }, checks: { current: 0, limit: null }, users: { current: 0, limit: null }, rules: { current: 0, limit: null }, channels: { current: 0, limit: null } } } })
    const calls = mockApi({})
    renderEdit()
    const hosts = await screen.findByLabelText('Hosts')
    fireEvent.change(hosts, { target: { value: '-1' } })
    fireEvent.click(screen.getByRole('button', { name: 'Guardar' }))
    expect(await screen.findByText(/entero >= 0/)).toBeTruthy()
    expect(calls.filter((c) => c.method === 'PATCH')).toHaveLength(0)
  })

  it('vaciar un campo lo manda como null (sin limite)', async () => {
    mockApi({ 'GET /groups/g1/usage': { data: { hosts: { current: 3, limit: 10 }, devices: { current: 0, limit: null }, checks: { current: 0, limit: null }, users: { current: 0, limit: null }, rules: { current: 0, limit: null }, channels: { current: 0, limit: null } } } })
    const calls = mockApi({ 'PATCH /groups/g1': { data: { group: GROUP } } })
    const onSaved = vi.fn()
    renderEdit(onSaved)
    const hosts = await screen.findByLabelText('Hosts')
    fireEvent.change(hosts, { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Guardar' }))
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    const body = calls.find((c) => c.method === 'PATCH')?.body as Record<string, unknown>
    expect(body?.max_hosts).toBeNull()
  })
})

describe('GroupForm al crear', () => {
  it('muestra la retención de detalle desde el alta, heredando por defecto', async () => {
    renderCreate()
    expect(await screen.findByLabelText('Retención de detalle (días)')).toBeTruthy()
    expect((screen.getByLabelText('Retención de detalle (días)') as HTMLInputElement).disabled).toBe(true)
    expect(await checkA11y()).toHaveNoViolations()
  })

  it('heredando no envía detail_retention_days', async () => {
    const calls = mockApi({ 'POST /groups': { status: 201, data: { group: { id: 'g1' } } } })
    const onSaved = vi.fn()
    renderCreate(onSaved)
    fireEvent.change(await screen.findByLabelText('Nombre'), { target: { value: 'Cliente A' } })
    fireEvent.click(screen.getByRole('button', { name: 'Guardar' }))
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    expect(calls[0].body).toEqual({ name: 'Cliente A', down_after_seconds: null, always_on: false })
  })

  it('con retención propia la envía sin pedir confirmación de reducción', async () => {
    const calls = mockApi({ 'POST /groups': { status: 201, data: { group: { id: 'g1' } } } })
    const onSaved = vi.fn()
    renderCreate(onSaved)
    fireEvent.change(await screen.findByLabelText('Nombre'), { target: { value: 'Cliente 90' } })
    fireEvent.click(screen.getByRole('checkbox', { name: /Heredar/ }))
    fireEvent.change(screen.getByLabelText('Retención de detalle (días)'), { target: { value: '90' } })
    fireEvent.click(screen.getByRole('button', { name: 'Guardar' }))
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    expect(calls[0].body).toEqual({ name: 'Cliente 90', down_after_seconds: null, always_on: false, detail_retention_days: 90 })
  })

  it('una retención fuera de rango no llama al servidor', async () => {
    const calls = mockApi({})
    renderCreate()
    fireEvent.change(await screen.findByLabelText('Nombre'), { target: { value: 'X' } })
    fireEvent.click(screen.getByRole('checkbox', { name: /Heredar/ }))
    fireEvent.change(screen.getByLabelText('Retención de detalle (días)'), { target: { value: '5' } })
    fireEvent.click(screen.getByRole('button', { name: 'Guardar' }))
    expect(await screen.findByText(/entre 8 y 730/i)).toBeTruthy()
    expect(calls).toHaveLength(0)
  })
})
