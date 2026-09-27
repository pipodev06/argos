// @vitest-environment jsdom
import { screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { checkA11y, renderWithRouter } from '@/test/utils'
import { StatusBlock } from './StatusBlock'

const COUNTS = { up: 12, down: 2, offline: 3, pending: 1, disabled: 0 }

describe('StatusBlock', () => {
  it('cinco tiles con cifra y enlace ?status= a la lista', () => {
    renderWithRouter(<StatusBlock id="b" title="Hosts" counts={COUNTS} to="/hosts" />)
    const hrefs = screen.getAllByRole('link').map((a) => a.getAttribute('href'))
    expect(hrefs).toEqual(['/hosts?status=up', '/hosts?status=down', '/hosts?status=offline', '/hosts?status=pending', '/hosts?status=disabled'])
    expect(screen.getByRole('link', { name: /Caídos/ }).textContent).toContain('2')
    expect(screen.getByRole('link', { name: /Activos/ }).textContent).toContain('12')
  })

  it('con caídos > 0 marca la alerta (barra danger); con 0 no', () => {
    const withDown = renderWithRouter(<StatusBlock id="b" title="Hosts" counts={COUNTS} to="/hosts" />)
    expect(withDown.container.querySelectorAll('.bg-danger').length).toBe(1)
    withDown.unmount()
    const none = renderWithRouter(<StatusBlock id="b" title="Hosts" counts={{ ...COUNTS, down: 0 }} to="/hosts" />)
    expect(none.container.querySelectorAll('.bg-danger').length).toBe(0)
  })

  it('sin datos muestra esqueletos y ningún enlace', () => {
    renderWithRouter(<StatusBlock id="b" title="Hosts" to="/hosts" />)
    expect(screen.queryAllByRole('link').length).toBe(0)
    expect(screen.getByRole('heading', { name: 'Hosts' })).toBeTruthy()
  })

  it('sin violaciones de accesibilidad', async () => {
    renderWithRouter(<StatusBlock id="b" title="Hosts" counts={COUNTS} to="/hosts" />)
    expect(await checkA11y()).toHaveNoViolations()
  })
})
