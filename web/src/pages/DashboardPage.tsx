import { lazy, useEffect } from 'react'
import { Route, Routes, useLocation } from 'react-router'
import { AppShell } from '@/features/layout/components/AppShell'

// Cada pantalla es su propio chunk (el Suspense vive en AppShell, alrededor del Outlet).
const AlertDetailPage = lazy(() => import('./AlertDetailPage').then((m) => ({ default: m.AlertDetailPage })))
const AlertsPage = lazy(() => import('./AlertsPage').then((m) => ({ default: m.AlertsPage })))
const ChecksPage = lazy(() => import('./ChecksPage').then((m) => ({ default: m.ChecksPage })))
const ChannelsPage = lazy(() => import('./ChannelsPage').then((m) => ({ default: m.ChannelsPage })))
const ConfigurationPage = lazy(() => import('./ConfigurationPage').then((m) => ({ default: m.ConfigurationPage })))
const DevicesPage = lazy(() => import('./DevicesPage').then((m) => ({ default: m.DevicesPage })))
const GroupsPage = lazy(() => import('./GroupsPage').then((m) => ({ default: m.GroupsPage })))
const ReportsPage = lazy(() => import('./ReportsPage').then((m) => ({ default: m.ReportsPage })))
const HostDetailPage = lazy(() => import('./HostDetailPage').then((m) => ({ default: m.HostDetailPage })))
const HostEventsPage = lazy(() => import('./HostEventsPage').then((m) => ({ default: m.HostEventsPage })))
const HostHistoryPage = lazy(() => import('./HostHistoryPage').then((m) => ({ default: m.HostHistoryPage })))
const HostsPage = lazy(() => import('./HostsPage').then((m) => ({ default: m.HostsPage })))
const InventoryPage = lazy(() => import('./InventoryPage').then((m) => ({ default: m.InventoryPage })))
const UpdatesPage = lazy(() => import('./UpdatesPage').then((m) => ({ default: m.UpdatesPage })))
const MaintenancePage = lazy(() => import('./MaintenancePage').then((m) => ({ default: m.MaintenancePage })))
const NotFoundPage = lazy(() => import('./NotFoundPage').then((m) => ({ default: m.NotFoundPage })))
const RulesPage = lazy(() => import('./RulesPage').then((m) => ({ default: m.RulesPage })))
const SummaryPage = lazy(() => import('./SummaryPage').then((m) => ({ default: m.SummaryPage })))
const UsersPage = lazy(() => import('./UsersPage').then((m) => ({ default: m.UsersPage })))
const AuditPage = lazy(() => import('./AuditPage').then((m) => ({ default: m.AuditPage })))
const SnmpProfilesPage = lazy(() => import('./SnmpProfilesPage').then((m) => ({ default: m.SnmpProfilesPage })))
const InstructionsPage = lazy(() => import('./InstructionsPage').then((m) => ({ default: m.InstructionsPage })))

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
        <Route path="chequeos" element={<ChecksPage />} />
        <Route path="informes" element={<ReportsPage />} />
        <Route path="alertas" element={<AlertsPage />} />
        <Route path="alertas/reglas" element={<RulesPage />} />
        <Route path="alertas/mantenimiento" element={<MaintenancePage />} />
        <Route path="alertas/canales" element={<ChannelsPage />} />
        <Route path="alertas/:id" element={<AlertDetailPage />} />
        <Route path="usuarios" element={<UsersPage />} />
        <Route path="perfiles-snmp" element={<SnmpProfilesPage />} />
        <Route path="configuracion" element={<ConfigurationPage />} />
        <Route path="auditoria" element={<AuditPage />} />
        <Route path="instrucciones" element={<InstructionsPage />} />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  )
}
