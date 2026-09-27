// @vitest-environment jsdom
import { fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { checkA11y, mockApi } from '@/test/utils'
import { GroupFormDialog } from './GroupFormDialog'

afterEach(() => vi.unstubAllGlobals())

function renderCreate(onSaved = () => {}) {
  render(<GroupFormDialog group={null} open defaultDownAfter={300} onClose={() => {}} onSaved={onSaved} />)
}

describe('GroupFormDialog al crear', () => {
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
