import { IconMoon, IconSun } from '@tabler/icons-react'
import { Button } from '@/components/ui/button'
import { useTheme } from '../hooks/useTheme'

// Unico mecanismo de cambio de modo (no se sigue prefers-color-scheme). Visible tambien sin sesion.
export function ThemeToggle() {
  const { theme, toggleTheme } = useTheme()
  const Icon = theme === 'dark' ? IconSun : IconMoon

  return (
    <Button
      type="button"
      variant="ghost"
      size="icon-sm"
      onClick={toggleTheme}
      aria-label={theme === 'dark' ? 'Cambiar a modo claro' : 'Cambiar a modo oscuro'}
    >
      <Icon stroke={1.5} aria-hidden />
    </Button>
  )
}
