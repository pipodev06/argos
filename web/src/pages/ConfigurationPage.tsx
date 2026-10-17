import { IconLock } from '@tabler/icons-react'
import { EmptyState } from '@/components/ui/empty-state'
import { InlineError } from '@/components/ui/inline-error'
import { PageHeader } from '@/components/ui/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { useGroups } from '@/features/groups/hooks/useGroups'
import { RetentionExceptions } from '@/features/retention/components/RetentionExceptions'
import { RetentionSection } from '@/features/retention/components/RetentionSection'
import { useRetention } from '@/features/retention/hooks/useRetention'
import { SnmpProfilesSection } from '@/features/snmp-profiles/components/SnmpProfilesSection'
import { ServiceStatusSection } from '@/features/system-status/components/ServiceStatusSection'
import { useSummary } from '@/features/summary/hooks/useSummary'

/** Solo administradores: un viewer ve el aviso y no se monta nada que llame a la API. */
export function ConfigurationPage() {
  const { user } = useAuth()
  if (user?.role !== 'admin') {
    return (
      <>
        <PageHeader title="Configuración" />
        <EmptyState Icon={IconLock} title="No tienes permiso" description="Solo los administradores cambian la configuración." />
      </>
    )
  }
  return <ConfigurationAdmin />
}

function ConfigurationAdmin() {
  const retention = useRetention()
  const groups = useGroups()
  const summary = useSummary()
  // Hosts con agente (los dispositivos no guardan metricas de detalle).
  const hostCount = summary.data ? Object.values(summary.data.counts).reduce((a, b) => a + b, 0) : null

  return (
    <>
      <PageHeader title="Configuración" />
      <div className="flex flex-col gap-6" aria-busy={retention.loading}>
        <ServiceStatusSection />
        {retention.error && <InlineError>{retention.error}</InlineError>}
        {retention.loading && !retention.data && <Skeleton className="h-48 w-full" />}
        {retention.data && (
          <RetentionSection settings={retention.data} hostCount={hostCount} onSaved={() => { void retention.refresh(); void groups.refresh() }} />
        )}
        {groups.error && <InlineError>{groups.error}</InlineError>}
        {groups.groups && <RetentionExceptions groups={groups.groups.items} />}
        <SnmpProfilesSection />
      </div>
    </>
  )
}
