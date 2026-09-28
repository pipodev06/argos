// @vitest-environment jsdom
import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { FlappingBadge } from './AlertBadges'

describe('FlappingBadge', () => {
  it('aparece solo si flapping', () => {
    const { rerender } = render(<FlappingBadge flapping={false} />)
    expect(screen.queryByText('Intermitente')).toBeNull()
    rerender(<FlappingBadge flapping />)
    expect(screen.getByText('Intermitente')).toBeTruthy()
  })
})
