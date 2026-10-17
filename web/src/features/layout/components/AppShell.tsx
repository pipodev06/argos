import { Suspense, type MouseEvent } from 'react'
import { Link, Outlet, useLocation } from 'react-router'
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from '@/components/ui/breadcrumb'
import { Separator } from '@/components/ui/separator'
import { SidebarInset, SidebarProvider, SidebarTrigger } from '@/components/ui/sidebar'
import { Skeleton } from '@/components/ui/skeleton'
import { TooltipProvider } from '@/components/ui/tooltip'
import { useSidebarCollapsed } from '../hooks/useSidebarCollapsed'
import { navItemForPath } from '../nav-items'
import { AppSidebar } from './Sidebar'

/** Migas de la barra superior: seccion › pagina (› "Detalle" en rutas hijas de la pagina). */
function PageBreadcrumb() {
  const { pathname } = useLocation()
  const item = navItemForPath(pathname)
  if (!item) return null
  const nested = pathname !== item.to
  return (
    <Breadcrumb>
      <BreadcrumbList>
        <BreadcrumbItem className="hidden md:block">{item.section}</BreadcrumbItem>
        <BreadcrumbSeparator className="hidden md:block" />
        <BreadcrumbItem>
          {nested ? (
            <BreadcrumbLink asChild>
              <Link to={item.to}>{item.label}</Link>
            </BreadcrumbLink>
          ) : (
            <BreadcrumbPage>{item.label}</BreadcrumbPage>
          )}
        </BreadcrumbItem>
        {nested && (
          <>
            <BreadcrumbSeparator />
            <BreadcrumbItem>
              <BreadcrumbPage>Detalle</BreadcrumbPage>
            </BreadcrumbItem>
          </>
        )}
      </BreadcrumbList>
    </Breadcrumb>
  )
}

// Shell sobre el bloque sidebar-07 de shadcn: sidebar 240px / 56px y barra superior con contraer + migas.
// El login y el cambio forzado de clave NO usan el shell.
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
          <header className="sticky top-0 z-30 flex h-12 shrink-0 items-center gap-2 border-b border-border bg-bg px-4">
            <SidebarTrigger className="-ml-1" aria-label="Mostrar u ocultar el menú" />
            <Separator orientation="vertical" className="mr-2 data-[orientation=vertical]:h-4" />
            <PageBreadcrumb />
          </header>
          <main id="main" tabIndex={-1} className="min-w-0 flex-1 outline-none">
            <div className="w-full px-4 py-6 md:px-8">
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
