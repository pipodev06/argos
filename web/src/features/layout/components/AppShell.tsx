import { Suspense, useState, type MouseEvent } from 'react'
import { IconMenu2 } from '@tabler/icons-react'
import { cn } from 'cn'
import { Outlet } from 'react-router'
import { AppLogo } from '@/components/ui/app-logo'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { Sheet, SheetContent, SheetTitle, SheetTrigger } from '@/components/ui/sheet'
import { TooltipProvider } from '@/components/ui/tooltip'
import { ThemeToggle } from '@/features/theme/components/ThemeToggle'
import { useSidebarCollapsed } from '../hooks/useSidebarCollapsed'
import { SidebarContent } from './Sidebar'

// Shell de las pantallas con sesion: sidebar (240px / 56px) desde `md`; en movil barra superior de 48px + drawer.
// El login y el cambio forzado de clave NO usan el shell.
export function AppShell() {
  const { collapsed, toggle } = useSidebarCollapsed()
  const [drawerOpen, setDrawerOpen] = useState(false)

  // El ancla "#main" ensuciaria la URL de react-router; se enfoca el <main> directamente.
  function skipToContent(event: MouseEvent<HTMLAnchorElement>) {
    event.preventDefault()
    document.getElementById('main')?.focus()
  }

  return (
    <TooltipProvider>
      <div className="min-h-svh bg-bg text-text md:flex">
        <a
          href="#main"
          onClick={skipToContent}
          className="sr-only focus:not-sr-only focus:fixed focus:top-2 focus:left-2 focus:z-[60] focus:rounded-md focus:border focus:border-border focus:bg-surface focus:px-3 focus:py-2 focus:text-sm focus:ring-2 focus:ring-ring"
        >
          Saltar al contenido
        </a>

        {/* Movil (<md): barra superior + drawer */}
        <Sheet open={drawerOpen} onOpenChange={setDrawerOpen}>
          <header className="sticky top-0 z-30 flex h-12 items-center gap-2 border-b border-border bg-surface px-2 md:hidden">
            <SheetTrigger asChild>
              <Button type="button" variant="ghost" size="icon" aria-label="Abrir menú">
                <IconMenu2 stroke={1.5} aria-hidden />
              </Button>
            </SheetTrigger>
            <AppLogo />
            <div className="ml-auto">
              <ThemeToggle />
            </div>
          </header>
          <SheetContent side="left" aria-describedby={undefined} className="gap-0">
            <SheetTitle className="sr-only">Menú</SheetTitle>
            <SidebarContent onNavigate={() => setDrawerOpen(false)} />
          </SheetContent>
        </Sheet>

        {/* Escritorio (>=md) */}
        <aside
          className={cn(
            'sticky top-0 hidden h-svh shrink-0 border-r border-border bg-surface md:block',
            collapsed ? 'w-14' : 'w-60',
          )}
        >
          <SidebarContent collapsed={collapsed} onToggleCollapsed={toggle} />
        </aside>

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
      </div>
    </TooltipProvider>
  )
}
