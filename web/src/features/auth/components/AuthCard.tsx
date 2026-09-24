import type { ReactNode } from 'react'
import { ThemeToggle } from '@/features/theme/components/ThemeToggle'

interface AuthCardProps {
  title: string
  description?: string
  children: ReactNode
}

// Pantalla completa centrada para login y cambio forzado de clave (borde 1px, sin sombra).
export function AuthCard({ title, description, children }: AuthCardProps) {
  return (
    <main className="relative flex min-h-svh items-center justify-center bg-bg px-4 py-8 text-text">
      <div className="absolute top-4 right-4">
        <ThemeToggle />
      </div>
      <div className="grid w-full max-w-sm gap-5 rounded-md border border-border bg-surface p-6">
        <div className="grid gap-1">
          <span className="text-sm font-semibold text-text-muted">argos</span>
          <h1 className="text-lg font-semibold">{title}</h1>
          {description && <p className="text-sm text-text-muted">{description}</p>}
        </div>
        {children}
      </div>
    </main>
  )
}
