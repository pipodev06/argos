// @vitest-environment jsdom
import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it, vi } from 'vitest'
import { checkA11y } from '@/test/utils'
import { ConfirmDialog } from './confirm-dialog'
import { FormField } from './form-field'

function Harness({ onConfirm, busy = false, error = null }: { onConfirm: () => void; busy?: boolean; error?: string | null }) {
  const [typed, setTyped] = useState('')
  return (
    <ConfirmDialog
      open
      title="Eliminar a web-01"
      description="Irreversible."
      confirmLabel="Eliminar"
      busy={busy}
      error={error}
      confirmDisabled={typed !== 'web-01'}
      onConfirm={onConfirm}
      onClose={() => {}}
    >
      <FormField id="typed" label='Escribe "web-01" para confirmar' value={typed} onChange={(e) => setTyped(e.target.value)} />
    </ConfirmDialog>
  )
}

const confirmBtn = () => screen.getByRole('button', { name: 'Eliminar' }) as HTMLButtonElement

describe('ConfirmDialog', () => {
  it('confirmar queda deshabilitado hasta teclear el nombre exacto', () => {
    const onConfirm = vi.fn()
    render(<Harness onConfirm={onConfirm} />)
    const input = screen.getByLabelText(/Escribe/)
    expect(confirmBtn().disabled).toBe(true)
    fireEvent.change(input, { target: { value: 'web-0' } })
    expect(confirmBtn().disabled).toBe(true)
    fireEvent.change(input, { target: { value: 'web-01' } })
    expect(confirmBtn().disabled).toBe(false)
    fireEvent.click(confirmBtn())
    expect(onConfirm).toHaveBeenCalledTimes(1)
  })

  it('ocupado muestra "Procesando..." y bloquea; el error se anuncia', () => {
    render(<Harness onConfirm={() => {}} busy error="Falló" />)
    expect((screen.getByRole('button', { name: 'Procesando...' }) as HTMLButtonElement).disabled).toBe(true)
    expect(screen.getByText('Falló')).toBeTruthy()
  })

  it('sin violaciones de accesibilidad', async () => {
    render(<Harness onConfirm={() => {}} />)
    expect(await checkA11y()).toHaveNoViolations()
  })
})
