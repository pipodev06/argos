// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { checkA11y, mockApi } from '@/test/utils'
import type { ManagedUser } from '../types/user.types'
import { CreateUserDialog } from './CreateUserDialog'
import { DeleteUserDialog } from './DeleteUserDialog'

afterEach(() => vi.unstubAllGlobals())

const GROUPS = {
  items: [{ id: 'g1', name: 'Cliente A', down_after_seconds: null, always_on: false, host_count: 2, detail_retention_days: null, effective_detail_days: 30, retention_source: 'global' }],
  ungrouped: { host_count: 0 },
  defaults: { down_after_seconds: 300 },
}

const USER: ManagedUser = {
  id: 'u1',
  username: 'ana',
  role: 'viewer',
  must_change_password: false,
  disabled: false,
  last_login_at: null,
  created_at: '2026-09-01T00:00:00Z',
  all_groups: false,
  group_ids: [],
  can_inventory: false,
  can_updates: false,
  can_ack: false,
}

const NO_VE_NADA = /no verá nada/
const ACCESO_TOTAL = /Da acceso a TODOS/

describe('CreateUserDialog', () => {
  it('viewer sin grupos avisa que no verá nada', async () => {
    mockApi({ 'GET /groups': { data: GROUPS } })
    render(<CreateUserDialog open onClose={() => {}} onSaved={() => {}} />)
    expect(screen.getByRole('status').textContent).toMatch(NO_VE_NADA)
    await screen.findByText('Cliente A')
  })

  it('"todos los grupos" cambia el aviso: acceso total en vez de "no verá nada"', async () => {
    mockApi({ 'GET /groups': { data: GROUPS } })
    render(<CreateUserDialog open onClose={() => {}} onSaved={() => {}} />)
    fireEvent.click(screen.getByRole('radio', { name: /Todos los grupos/ }))
    expect(screen.getByText(ACCESO_TOTAL)).toBeTruthy()
    expect(screen.queryByText(NO_VE_NADA)).toBeNull()
  })

  it('arma el payload: usuario, rol viewer, all_groups y permisos', async () => {
    const calls = mockApi({ 'GET /groups': { data: GROUPS }, 'POST /users': { data: { user: USER } } })
    const onSaved = vi.fn()
    render(<CreateUserDialog open onClose={() => {}} onSaved={onSaved} />)
    fireEvent.change(screen.getByLabelText('Usuario'), { target: { value: '  ana  ' } })
    fireEvent.change(screen.getByLabelText('Contraseña inicial'), { target: { value: 'una-frase-larga-123' } })
    fireEvent.click(screen.getByRole('radio', { name: /Todos los grupos/ }))
    fireEvent.click(screen.getByRole('checkbox', { name: /Reconocer alertas/ }))
    fireEvent.click(screen.getByRole('button', { name: 'Crear usuario' }))
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    expect(calls.find((c) => c.method === 'POST')?.body).toEqual({
      username: 'ana',
      password: 'una-frase-larga-123',
      role: 'viewer',
      all_groups: true,
      can_inventory: false,
      can_updates: false,
      can_ack: true,
    })
  })

  it('valida largo de usuario y contraseña sin llamar al servidor', () => {
    const calls = mockApi({ 'GET /groups': { data: GROUPS } })
    render(<CreateUserDialog open onClose={() => {}} onSaved={() => {}} />)
    fireEvent.click(screen.getByRole('button', { name: 'Crear usuario' }))
    expect(screen.getByText('Entre 3 y 64 caracteres.')).toBeTruthy()
    expect(calls.some((c) => c.method === 'POST')).toBe(false)
  })

  it('sin violaciones de accesibilidad', async () => {
    mockApi({ 'GET /groups': { data: GROUPS } })
    render(<CreateUserDialog open onClose={() => {}} onSaved={() => {}} />)
    await screen.findByText('Cliente A')
    expect(await checkA11y()).toHaveNoViolations()
  })
})

describe('DeleteUserDialog', () => {
  const del = () => screen.getByRole('button', { name: 'Eliminar usuario' }) as HTMLButtonElement

  it('exige teclear el nombre y borra al confirmar', async () => {
    const calls = mockApi({ 'DELETE /users/u1': { data: null } })
    const onSaved = vi.fn()
    render(<DeleteUserDialog user={USER} onClose={() => {}} onSaved={onSaved} />)
    expect(del().disabled).toBe(true)
    fireEvent.change(screen.getByLabelText(/Escribe "ana"/), { target: { value: 'ana' } })
    expect(del().disabled).toBe(false)
    fireEvent.click(del())
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
    expect(calls.map((c) => `${c.method} ${c.path}`)).toEqual(['DELETE /users/u1'])
  })

  it('traduce last_admin a un mensaje claro', async () => {
    mockApi({ 'DELETE /users/u1': { status: 409, error: { code: 'last_admin', message: 'x' } } })
    render(<DeleteUserDialog user={USER} onClose={() => {}} onSaved={() => {}} />)
    fireEvent.change(screen.getByLabelText(/Escribe "ana"/), { target: { value: 'ana' } })
    fireEvent.click(del())
    expect(await screen.findByText('Debe quedar al menos un administrador activo.')).toBeTruthy()
  })

  it('sin violaciones de accesibilidad', async () => {
    render(<DeleteUserDialog user={USER} onClose={() => {}} onSaved={() => {}} />)
    expect(await checkA11y()).toHaveNoViolations()
  })
})
