import { Suspense, type MouseEvent } from 'react'
import { Outlet } from 'react-router'
import { AppLogo } from '@/components/ui/app-logo'
import { SidebarInset, SidebarProvider, SidebarTrigger } from '@/components/ui/sidebar'
import { Skeleton } from '@/components/ui/skeleton'
import { TooltipProvider } from '@/components/ui/tooltip'
import { ThemeToggle } from '@/features/theme/components/ThemeToggle'
import { useSidebarCollapsed } from '../hooks/useSidebarCollapsed'
import { AppSidebar } from './Sidebar'

// Shell de las pantallas con sesion sobre el Sidebar de shadcn: 240px / 56px desde `md`; en movil barra superior
// de 48px + drawer. El login y el cambio forzado de clave NO usan el shell.
export function AppShell() {
  const { collapsed, setCollapsed } = useSidebarCollapsed()

  // El ancla "#main" ensuciaria la URL de react-router; se enfoca el <main> directamente.
  function skipToContent(event: MouseEvent<HTMLAnchorElement>) {
    event.preventDefault()
    document.getElementById('main')?.focus()
  }

  return (
    <TooltipProvider>
      <SidebarProvider open={!collapsed} onOpenChange={(open) => setCollapsed(!open)}>
        <a
          href="#main"
          onClick={skipToContent}
          className="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-[60] focus:rounded-md focus:border focus:border-border focus:bg-surface focus:px-3 focus:py-2 focus:text-sm focus:ring-2 focus:ring-ring"
        >
          Saltar al contenido
        </a>
        <AppSidebar />
        <SidebarInset className="min-w-0 bg-bg text-text">
          {/* Movil (<md): barra superior; el drawer lo abre SidebarTrigger */}
          <header className="sticky top-0 z-30 flex h-12 items-center gap-2 border-b border-border bg-surface px-2 md:hidden">
            <SidebarTrigger aria-label="Abrir menú" />
            <AppLogo />
            <div className="ml-auto">
              <ThemeToggle />
            </div>
          </header>
          <main id="main" tabIndex={-1} className="min-w-0 flex-1 outline-none">
            <div className="w-full px-4 py-6 md:px-8 md:py-8">
              <Suspense
                fallback={
                  <div role="status" aria-label="Cargando">
                    <Skeleton className="h-64 w-full" />
                  </div>
                }
              >
                <Outlet />
              </Suspense>
            </div>
          </main>
        </SidebarInset>
      </SidebarProvider>
    </TooltipProvider>
  )
}
