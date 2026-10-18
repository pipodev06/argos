import { LockIcon } from 'lucide-react'
import { EmptyState } from '@/components/ui/empty-state'
import { InlineError } from '@/components/ui/inline-error'
import { PageHeader } from '@/components/ui/page-header'
import { Skeleton } from '@/components/ui/skeleton'
import { useAuth } from '@/features/auth/hooks/useAuth'
import { RetentionSection } from '@/features/retention/components/RetentionSection'
import { useRetention } from '@/features/retention/hooks/useRetention'
import { ServiceStatusSection } from '@/features/system-status/components/ServiceStatusSection'
import { useSummary } from '@/features/summary/hooks/useSummary'

/** Solo administradores: un viewer ve el aviso y no se monta nada que llame a la API. */
export function SystemPage() {
  const { user } = useAuth()
  if (user?.role !== 'admin') {
    return (
      <>
        <PageHeader title="Sistema" />
        <EmptyState Icon={LockIcon} title="Sin permiso" />
      </>
    )
  }
  return <SystemAdmin />
}

function SystemAdmin() {
  const retention = useRetention()
  const summary = useSummary()
  // Hosts con agente (los dispositivos no guardan metricas de detalle).
  const hostCount = summary.data ? Object.values(summary.data.counts).reduce((a, b) => a + b, 0) : null

  return (
    <>
      <PageHeader title="Sistema" />
      <div className="flex flex-col gap-3" aria-busy={retention.loading}>
        <ServiceStatusSection />
        {retention.error && <InlineError>{retention.error}</InlineError>}
        {retention.loading && !retention.data && <Skeleton className="h-48 w-full" />}
        {retention.data && (
          <RetentionSection settings={retention.data} hostCount={hostCount} onSaved={() => void retention.refresh()} />
        )}
      </div>
    </>
  )
}
