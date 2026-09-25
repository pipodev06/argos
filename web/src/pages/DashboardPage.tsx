import { useEffect } from 'react'
import { Route, Routes, useLocation } from 'react-router'
import { AppShell } from '@/features/layout/components/AppShell'
import { AlertDetailPage } from './AlertDetailPage'
import { AlertsPage } from './AlertsPage'
import { ChecksPage } from './ChecksPage'
import { ChannelsPage } from './ChannelsPage'
import { GroupsPage } from './GroupsPage'
import { ReportsPage } from './ReportsPage'
import { HostDetailPage } from './HostDetailPage'
import { HostsPage } from './HostsPage'
import { InventoryPage } from './InventoryPage'
import { MaintenancePage } from './MaintenancePage'
import { NotFoundPage } from './NotFoundPage'
import { RulesPage } from './RulesPage'
import { SummaryPage } from './SummaryPage'
import { UsersPage } from './UsersPage'
import { AuditPage } from './AuditPage'

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
        <Route path="grupos" element={<GroupsPage />} />
        <Route path="chequeos" element={<ChecksPage />} />
        <Route path="informes" element={<ReportsPage />} />
        <Route path="alertas" element={<AlertsPage />} />
        <Route path="alertas/reglas" element={<RulesPage />} />
        <Route path="alertas/mantenimiento" element={<MaintenancePage />} />
        <Route path="alertas/canales" element={<ChannelsPage />} />
        <Route path="alertas/:id" element={<AlertDetailPage />} />
        <Route path="usuarios" element={<UsersPage />} />
        <Route path="auditoria" element={<AuditPage />} />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  )
}
