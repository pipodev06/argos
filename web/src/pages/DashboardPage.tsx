import { lazy, useEffect } from 'react'
import { Route, Routes, useLocation } from 'react-router'
import { AppShell } from '@/features/layout/components/AppShell'
import { AlertDetailPage } from './AlertDetailPage'
import { AlertsPage } from './AlertsPage'
import { AuditPage } from './AuditPage'
import { ChannelsPage } from './ChannelsPage'
import { CheckFormPage } from './CheckFormPage'
import { ChecksPage } from './ChecksPage'
import { ConfigurationPage } from './ConfigurationPage'
import { DevicesPage } from './DevicesPage'
import { GroupFormPage } from './GroupFormPage'
import { GroupsPage } from './GroupsPage'
import { HostEventsPage } from './HostEventsPage'
import { HostsPage } from './HostsPage'
import { InstructionsPage } from './InstructionsPage'
import { InventoryPage } from './InventoryPage'
import { MaintenancePage } from './MaintenancePage'
import { NotFoundPage } from './NotFoundPage'
import { ReportsPage } from './ReportsPage'
import { RuleFormPage } from './RuleFormPage'
import { RulesPage } from './RulesPage'
import { SummaryPage } from './SummaryPage'
import { UpdatesPage } from './UpdatesPage'
import { UsersPage } from './UsersPage'

// Pantallas livianas en el bundle principal (navegar no espera descargas). Solo las que usan gráficas (Recharts,
// ~350 KB) se cargan aparte; el Suspense vive en AppShell, alrededor del Outlet.
const HostDetailPage = lazy(() => import('./HostDetailPage').then((m) => ({ default: m.HostDetailPage })))
const HostHistoryPage = lazy(() => import('./HostHistoryPage').then((m) => ({ default: m.HostHistoryPage })))

export function DashboardPage() {
  const { pathname } = useLocation()

  // Al navegar entre pantallas se vuelve arriba (en modo declarativo no hay ScrollRestoration).
  useEffect(() => {
    window.scrollTo(0, 0)
  }, [pathname])

  return (
    <Routes>
      <Route element={<AppShell />}>
        <Route index element={<SummaryPage />} />
        <Route path="hosts" element={<HostsPage />} />
        <Route path="hosts/:id" element={<HostDetailPage />} />
        <Route path="hosts/:id/inventario" element={<InventoryPage />} />
        <Route path="hosts/:id/estados" element={<HostEventsPage />} />
        <Route path="hosts/:id/historial" element={<HostHistoryPage />} />
        <Route path="parches" element={<UpdatesPage />} />
        <Route path="dispositivos" element={<DevicesPage />} />
        <Route path="grupos" element={<GroupsPage />} />
        <Route path="grupos/nuevo" element={<GroupFormPage />} />
        <Route path="grupos/:id" element={<GroupFormPage />} />
        <Route path="chequeos" element={<ChecksPage />} />
        <Route path="chequeos/nuevo" element={<CheckFormPage />} />
        <Route path="chequeos/:id" element={<CheckFormPage />} />
        <Route path="informes" element={<ReportsPage />} />
        <Route path="alertas" element={<AlertsPage />} />
        <Route path="alertas/reglas" element={<RulesPage />} />
        <Route path="alertas/reglas/nueva" element={<RuleFormPage />} />
        <Route path="alertas/reglas/:id" element={<RuleFormPage />} />
        <Route path="alertas/mantenimiento" element={<MaintenancePage />} />
        <Route path="alertas/canales" element={<ChannelsPage />} />
        <Route path="alertas/:id" element={<AlertDetailPage />} />
        <Route path="usuarios" element={<UsersPage />} />
        <Route path="configuracion" element={<ConfigurationPage />} />
        <Route path="auditoria" element={<AuditPage />} />
        <Route path="instrucciones" element={<InstructionsPage />} />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  )
}
