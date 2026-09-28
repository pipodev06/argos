// @vitest-environment jsdom
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { EscalatedBadge, EscalationBadge, FlappingBadge } from './AlertBadges'

describe('FlappingBadge', () => {
  it('aparece solo si flapping', () => {
    const { rerender } = render(<FlappingBadge flapping={false} />)
    expect(screen.queryByText('Intermitente')).toBeNull()
    rerender(<FlappingBadge flapping />)
    expect(screen.getByText('Intermitente')).toBeTruthy()
  })
})

describe('EscalatedBadge', () => {
  it('aparece solo si escalated_at', () => {
    const { rerender } = render(<EscalatedBadge escalatedAt={null} />)
    expect(screen.queryByText('Escalada')).toBeNull()
    rerender(<EscalatedBadge escalatedAt="2026-09-27T10:00:00Z" />)
    expect(screen.getByText('Escalada')).toBeTruthy()
  })
})

describe('EscalationBadge', () => {
  it('muestra los minutos solo si hay plazo', () => {
    const { rerender } = render(<EscalationBadge seconds={null} />)
    expect(screen.queryByText(/Escala a los/)).toBeNull()
    rerender(<EscalationBadge seconds={900} />)
    expect(screen.getByText('Escala a los 15 min')).toBeTruthy()
  })
})
