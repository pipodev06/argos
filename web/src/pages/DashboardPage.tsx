import { lazy, useEffect } from 'react'
import { Navigate, Route, Routes, useLocation, useParams } from 'react-router'
import { AppShell } from '@/features/layout/components/AppShell'
import { AlertDetailPage } from './AlertDetailPage'
import { AlertsPage } from './AlertsPage'
import { AuditPage } from './AuditPage'
import { ChannelsPage } from './ChannelsPage'
import { CheckFormPage } from './CheckFormPage'
import { ChecksPage } from './ChecksPage'
import { DevicesPage } from './DevicesPage'
import { GroupFormPage } from './GroupFormPage'
import { GroupsPage } from './GroupsPage'
import { DeviceDetailPage } from './DeviceDetailPage'
import { HostDetailPage } from './HostDetailPage'
import { HostsPage } from './HostsPage'
import { MaintenancePage } from './MaintenancePage'
import { NotFoundPage } from './NotFoundPage'
import { ReportsPage } from './ReportsPage'
import { RuleFormPage } from './RuleFormPage'
import { RulesPage } from './RulesPage'
import { SummaryPage } from './SummaryPage'
import { UpdatesPage } from './UpdatesPage'
import { SnmpProfilesPage } from './SnmpProfilesPage'
import { SystemPage } from './SystemPage'
import { UsersPage } from './UsersPage'
import {
  HostChecksTab,
  HostDevicesTab,
  HostEventsTab,
  HostInventoryTab,
  HostSummaryTab,
  HostUpdatesTab,
} from '@/features/hosts/components/HostSections'

// Todo es estatico en el bundle principal salvo la ruta Métricas del host: es la unica que usa gráficas (Recharts,
// ~350 KB). El Suspense vive en HostLayout, alrededor de su Outlet.
const HostMetricsTab = lazy(() => import('@/features/hosts/components/HostMetricsTab').then((m) => ({ default: m.HostMetricsTab })))

/** `/hosts/:id/estados` (URL vieja) ahora es la pestaña Historial. */
function LegacyHostEvents() {
  const { id = '' } = useParams()
  return <Navigate to={`/hosts/${encodeURIComponent(id)}/historial`} replace />
}

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
        <Route path="hosts/:id" element={<HostDetailPage />}>
          <Route index element={<HostSummaryTab />} />
          <Route path="metricas" element={<HostMetricsTab />} />
          <Route path="chequeos" element={<HostChecksTab />} />
          <Route path="dispositivos" element={<HostDevicesTab />} />
          <Route path="parches" element={<HostUpdatesTab />} />
          <Route path="inventario" element={<HostInventoryTab />} />
          <Route path="historial" element={<HostEventsTab />} />
          <Route path="estados" element={<LegacyHostEvents />} />
        </Route>
        <Route path="parches" element={<UpdatesPage />} />
        <Route path="dispositivos" element={<DevicesPage />} />
        <Route path="dispositivos/:id" element={<DeviceDetailPage />} />
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
        <Route path="sistema" element={<SystemPage />} />
        <Route path="configuracion" element={<Navigate to="/sistema" replace />} />
        <Route path="perfiles-snmp" element={<SnmpProfilesPage />} />
        <Route path="auditoria" element={<AuditPage />} />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  )
}
